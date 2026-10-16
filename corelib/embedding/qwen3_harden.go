package embedding

// qwen3_harden.go — defensive loading for the single shipped embedder
// (Qwen3-Embedding-0.6B): path resolution, a low-memory guard and a
// failure backoff for lazy (re)loads. Every failure degrades to "no local
// embedder" (callers fall back to rules + BM25 / remote); nothing panics.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
)

// ---------------------------------------------------------------- paths

// Qwen3PathCandidate is one location ResolveQwen3ModelPath considered.
type Qwen3PathCandidate struct {
	Path   string
	Source string // explicit | env | slot-dir | models-dir | exe-dir
	Skip   string // why it was not used ("" = chosen)
}

// test hooks
var (
	executableFn    = os.Executable
	peekArchFn      = gguf.PeekArchitecture
	qwen3ExpectSize = func() int64 { return Qwen3ModelSpec.Size }
)

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

// ResolveQwen3ModelPath finds the Qwen3 GGUF to load. Order:
//  1. modelPath itself when it is a qwen3 GGUF (callers that pass the file)
//  2. MACLAW_EMBED_MODEL_PATH (file, or a directory holding Qwen3ModelFilename; ~ expanded)
//  3. Qwen3ModelFilename next to modelPath (the host's model slot)
//  4. Qwen3ModelFilename in DefaultModelsDir()
//  5. <exe dir>/models/ and <exe dir>/ (portable installs)
//
// A conventionally named file whose size differs from the release manifest
// is skipped as partial/corrupt (the fetcher repairs it); explicit / env
// files are trusted as-is. Returns "" when nothing usable exists.
func ResolveQwen3ModelPath(modelPath string) (string, []Qwen3PathCandidate) {
	var tried []Qwen3PathCandidate
	seen := map[string]bool{}
	try := func(p, src string, checkSize bool) bool {
		p = strings.TrimSpace(p)
		if p == "" {
			return false
		}
		p = filepath.Clean(expandHome(p))
		if seen[p] {
			return false
		}
		seen[p] = true
		c := Qwen3PathCandidate{Path: p, Source: src}
		fi, err := os.Stat(p)
		switch {
		case err != nil:
			c.Skip = "missing"
		case fi.IsDir():
			c.Skip = "is a directory"
		case fi.Size() == 0:
			c.Skip = "empty"
		case checkSize && qwen3ExpectSize() > 0 && fi.Size() != qwen3ExpectSize():
			c.Skip = fmt.Sprintf("size %d != release %d (partial or corrupt)", fi.Size(), qwen3ExpectSize())
		default:
			if arch, err := peekArchFn(p); err != nil {
				c.Skip = "unreadable: " + err.Error()
			} else if arch != "qwen3" {
				c.Skip = fmt.Sprintf("architecture %q", arch)
			}
		}
		tried = append(tried, c)
		return c.Skip == ""
	}
	if mp := strings.TrimSpace(modelPath); mp != "" {
		if fi, err := os.Stat(mp); err == nil && !fi.IsDir() && fi.Size() > 0 {
			if arch, err := peekArchFn(mp); err == nil && arch == "qwen3" {
				if try(mp, "explicit", false) {
					return filepath.Clean(mp), tried
				}
			}
		}
	}
	if env := strings.TrimSpace(os.Getenv(EmbedModelPathEnv)); env != "" {
		env = expandHome(env)
		if fi, err := os.Stat(env); err == nil && fi.IsDir() {
			env = filepath.Join(env, Qwen3ModelFilename)
		}
		if try(env, "env", false) {
			return filepath.Clean(env), tried
		}
	}
	dirs := []struct{ dir, src string }{}
	if strings.TrimSpace(modelPath) != "" {
		dirs = append(dirs, struct{ dir, src string }{filepath.Dir(modelPath), "slot-dir"})
	}
	if d := DefaultModelsDir(); d != "" {
		dirs = append(dirs, struct{ dir, src string }{d, "models-dir"})
	}
	if exe, err := executableFn(); err == nil && exe != "" {
		ed := filepath.Dir(exe)
		dirs = append(dirs, struct{ dir, src string }{filepath.Join(ed, "models"), "exe-dir"}, struct{ dir, src string }{ed, "exe-dir"})
	}
	for _, d := range dirs {
		p := filepath.Join(d.dir, Qwen3ModelFilename)
		if try(p, d.src, true) {
			return filepath.Clean(p), tried
		}
	}
	return "", tried
}

func describeCandidates(c []Qwen3PathCandidate) string {
	parts := make([]string, 0, len(c))
	for _, x := range c {
		parts = append(parts, x.Source+":"+x.Path+" ("+x.Skip+")")
	}
	return strings.Join(parts, "; ")
}

// ---------------------------------------------------------------- memory

// EmbedMemGuardEnv=0 disables the low-memory guard.
const EmbedMemGuardEnv = "MACLAW_EMBED_MEM_GUARD"

// EmbedMinFreeMBEnv overrides the headroom (MB) required on top of the model size.
const EmbedMinFreeMBEnv = "MACLAW_EMBED_MIN_FREE_MB"

// defaultEmbedHeadroomMB: scratch, tokenizer, RoPE tables, prefix KV and
// the rest of the app. The weights themselves cost ≈ file size (Q8P arena;
// mmap pages are released after packing, or stay as reclaimable page cache
// on the float path).
const defaultEmbedHeadroomMB = 384

// ErrLowMemory is returned (wrapped) when the guard refuses to load.
var ErrLowMemory = errors.New("insufficient free memory for the local embedding model")

var availableMemoryFn = availableMemory

// CheckModelMemory refuses to load a model of path's size when the OS reports
// less available memory than size + headroom. Unknown availability passes.
func CheckModelMemory(path string) error {
	if strings.TrimSpace(os.Getenv(EmbedMemGuardEnv)) == "0" {
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil // the loader reports the real error
	}
	headroom := uint64(defaultEmbedHeadroomMB)
	if v, err := strconv.ParseUint(strings.TrimSpace(os.Getenv(EmbedMinFreeMBEnv)), 10, 64); err == nil {
		headroom = v
	}
	need := uint64(fi.Size()) + headroom<<20
	avail, ok := availableMemoryFn()
	if !ok || avail >= need {
		return nil
	}
	return fmt.Errorf("%w: need ~%d MB, available %d MB (set %s=0 to override)", ErrLowMemory, need>>20, avail>>20, EmbedMemGuardEnv)
}

// ---------------------------------------------------------------- retry

// ErrLoadBackoff wraps the last load error while a retry is not yet due.
var ErrLoadBackoff = errors.New("embedder load backing off after failure")

const (
	qwen3RetryBase = 30 * time.Second
	qwen3RetryMax  = 10 * time.Minute
)

// loadGate rate-limits reload attempts of one model file after failures, so
// lazy callers (every intent/tool/topic call that finds no embedder) do not
// re-open a 600 MB GGUF on every request. A changed file (size/mtime — e.g.
// the background download finished) or ResetQwen3LoadBackoff retries at once.
type loadGate struct {
	mu       sync.Mutex
	key      string
	failures int
	next     time.Time
	lastErr  error
}

var (
	qwen3Gate = &loadGate{}
	gateNow   = time.Now
)

func fileGateKey(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return path + "|missing"
	}
	return fmt.Sprintf("%s|%d|%d", path, fi.Size(), fi.ModTime().UnixNano())
}

func (g *loadGate) allow(key string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if key != g.key {
		g.key, g.failures, g.next, g.lastErr = key, 0, time.Time{}, nil
		return nil
	}
	if g.failures == 0 || !gateNow().Before(g.next) {
		return nil
	}
	return fmt.Errorf("%w (retry in %s): %v", ErrLoadBackoff, g.next.Sub(gateNow()).Round(time.Second), g.lastErr)
}

func (g *loadGate) fail(key string, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if key != g.key {
		g.key, g.failures = key, 0
	}
	g.failures++
	d := qwen3RetryBase << uint(min(g.failures-1, 10))
	if d > qwen3RetryMax || d <= 0 {
		d = qwen3RetryMax
	}
	g.next, g.lastErr = gateNow().Add(d), err
}

func (g *loadGate) success(key string) {
	g.mu.Lock()
	g.key, g.failures, g.next, g.lastErr = key, 0, time.Time{}, nil
	g.mu.Unlock()
}

// ResetQwen3LoadBackoff makes the next load attempt run immediately (call
// after a model download or a settings change).
func ResetQwen3LoadBackoff() {
	qwen3Gate.mu.Lock()
	qwen3Gate.key, qwen3Gate.failures, qwen3Gate.next, qwen3Gate.lastErr = "", 0, time.Time{}, nil
	qwen3Gate.mu.Unlock()
}
