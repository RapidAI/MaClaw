package embedding

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/embedding/gguf"
	"github.com/RapidAI/CodeClaw/corelib/embedding/tensor"
)

// ---- path resolution ----

func TestResolveQwen3ModelPathOrder(t *testing.T) {
	hardenStubs(t)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	slot := t.TempDir()
	gp := filepath.Join(slot, DefaultModelFilename)
	base := t.TempDir()
	old, _ := BaseDirFunc.Load().(func() string)
	BaseDirFunc.Store(func() string { return base })
	defer BaseDirFunc.Store(func() string {
		if old != nil {
			return old()
		}
		return ""
	})
	modelsDir := filepath.Join(base, "models")
	os.MkdirAll(modelsDir, 0o755)
	write := func(p, c string) { os.MkdirAll(filepath.Dir(p), 0o755); os.WriteFile(p, []byte(c), 0o644) }

	if p, tried := ResolveQwen3ModelPath(gp); p != "" || len(tried) == 0 {
		t.Fatalf("nothing on disk: got %q tried=%v", p, tried)
	}
	// exe-dir (portable install) is the last resort
	exe, _ := executableFn()
	write(filepath.Join(filepath.Dir(exe), "models", Qwen3ModelFilename), "q")
	if p, _ := ResolveQwen3ModelPath(gp); p != filepath.Join(filepath.Dir(exe), "models", Qwen3ModelFilename) {
		t.Fatalf("exe-dir: %q", p)
	}
	// models-dir beats exe-dir
	write(filepath.Join(modelsDir, Qwen3ModelFilename), "q")
	if p, _ := ResolveQwen3ModelPath(gp); p != filepath.Join(modelsDir, Qwen3ModelFilename) {
		t.Fatalf("models-dir: %q", p)
	}
	// slot dir beats models-dir
	write(filepath.Join(slot, Qwen3ModelFilename), "q")
	if p, _ := ResolveQwen3ModelPath(gp); p != filepath.Join(slot, Qwen3ModelFilename) {
		t.Fatalf("slot-dir: %q", p)
	}
	// env directory beats the slot
	envDir := t.TempDir()
	write(filepath.Join(envDir, Qwen3ModelFilename), "q")
	t.Setenv(EmbedModelPathEnv, envDir)
	if p, _ := ResolveQwen3ModelPath(gp); p != filepath.Join(envDir, Qwen3ModelFilename) {
		t.Fatalf("env dir: %q", p)
	}
	// env file with a custom name
	custom := filepath.Join(t.TempDir(), "my-qwen.gguf")
	write(custom, "q")
	t.Setenv(EmbedModelPathEnv, custom)
	if p, _ := ResolveQwen3ModelPath(gp); p != custom {
		t.Fatalf("env file: %q", p)
	}
	// a broken env path falls through to the defaults instead of failing
	t.Setenv(EmbedModelPathEnv, filepath.Join(t.TempDir(), "gone.gguf"))
	p, tried := ResolveQwen3ModelPath(gp)
	if p != filepath.Join(slot, Qwen3ModelFilename) || tried[0].Source != "env" || tried[0].Skip != "missing" {
		t.Fatalf("env missing → slot: %q %+v", p, tried)
	}
	// explicit qwen3 file passed as the model path wins
	if p, _ := ResolveQwen3ModelPath(custom); p != custom {
		t.Fatalf("explicit: %q", p)
	}
}

func TestResolveQwen3ModelPathSkipsPartialWrongArchAndDirs(t *testing.T) {
	hardenStubs(t)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	slot := t.TempDir()
	gp := filepath.Join(slot, DefaultModelFilename)
	qp := filepath.Join(slot, Qwen3ModelFilename)

	qwen3ExpectSize = func() int64 { return 10 }
	os.WriteFile(qp, []byte("12345"), 0o644) // half-downloaded
	if p, tried := ResolveQwen3ModelPath(gp); p != "" || !strings.Contains(tried[0].Skip, "partial") {
		t.Fatalf("partial file must be skipped: %q %+v", p, tried)
	}
	os.WriteFile(qp, []byte("gemma....."), 0o644) // right size, wrong architecture
	peek := peekArchFn
	peekArchFn = func(string) (string, error) { return "gemma-embedding", nil }
	if p, tried := ResolveQwen3ModelPath(gp); p != "" || !strings.Contains(tried[0].Skip, "architecture") {
		t.Fatalf("wrong arch must be skipped: %q %+v", p, tried)
	}
	peekArchFn = peek
	os.Remove(qp)
	os.Mkdir(qp, 0o755)
	if p, tried := ResolveQwen3ModelPath(gp); p != "" || tried[0].Skip != "is a directory" {
		t.Fatalf("directory must be skipped: %q %+v", p, tried)
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		if got := expandHome("~/x.gguf"); got != filepath.Join(home, "x.gguf") {
			t.Fatalf("expandHome = %q", got)
		}
	}
}

// ---- lazy-load retry ----

func TestPreferredLazyRetryBackoff(t *testing.T) {
	now := hardenStubs(t)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	dir, gp := prefModels(t, true, false)
	spy := &loadSpy{qwenErr: errors.New("transient: sharing violation")}

	if _, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{}); err == nil || spy.qwenCalls != 1 {
		t.Fatalf("first attempt: err=%v calls=%d", err, spy.qwenCalls)
	}
	// Within the backoff window: no reload, degraded noop, nil error.
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil || !IsNoop(res.Embedder) || spy.qwenCalls != 1 || !errors.Is(res.QwenErr, ErrLoadBackoff) {
		t.Fatalf("backoff: res=%+v err=%v calls=%d", res, err, spy.qwenCalls)
	}
	// After the window the load is retried (and backs off longer on failure).
	*now = now.Add(qwen3RetryBase + time.Second)
	loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if spy.qwenCalls != 2 {
		t.Fatalf("retry after %s: calls=%d", qwen3RetryBase, spy.qwenCalls)
	}
	*now = now.Add(qwen3RetryBase + time.Second) // < 2×base: still backing off
	loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if spy.qwenCalls != 2 {
		t.Fatalf("exponential backoff: calls=%d", spy.qwenCalls)
	}
	// A replaced file (download finished) retries immediately.
	qp := filepath.Join(dir, Qwen3ModelFilename)
	os.WriteFile(qp, []byte("gguf-v2"), 0o644)
	os.Chtimes(qp, now.Add(time.Hour), now.Add(time.Hour))
	spy.qwenErr = nil
	res, err = loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if err != nil || res.Family != FamilyQwen3 || spy.qwenCalls != 3 {
		t.Fatalf("changed file: res=%+v err=%v calls=%d", res, err, spy.qwenCalls)
	}
	// ResetQwen3LoadBackoff also forces an immediate retry.
	spy.qwenErr = errors.New("again")
	loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	ResetQwen3LoadBackoff()
	loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if spy.qwenCalls != 5 {
		t.Fatalf("reset: calls=%d", spy.qwenCalls)
	}
}

// ---- low-memory guard ----

func TestPreferredLowMemoryGuard(t *testing.T) {
	hardenStubs(t)
	t.Setenv(EmbedModelEnv, "")
	t.Setenv(EmbedModelPathEnv, "")
	_, gp := prefModels(t, true, false)
	availableMemoryFn = func() (uint64, bool) { return 100 << 20, true } // 100 MB free
	spy := &loadSpy{}
	res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{})
	if !errors.Is(err, ErrLowMemory) || !IsNoop(res.Embedder) || spy.qwenCalls != 0 {
		t.Fatalf("guard: res=%+v err=%v calls=%d", res, err, spy.qwenCalls)
	}
	// Unknown availability (macOS) never blocks.
	ResetQwen3LoadBackoff()
	availableMemoryFn = func() (uint64, bool) { return 0, false }
	if res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{}); err != nil || res.Family != FamilyQwen3 {
		t.Fatalf("unknown memory: %+v %v", res, err)
	}
	// Explicit override.
	ResetQwen3LoadBackoff()
	availableMemoryFn = func() (uint64, bool) { return 100 << 20, true }
	t.Setenv(EmbedMemGuardEnv, "0")
	if res, err := loadPreferredWith(spy.loaders(), gp, PreferredOptions{}); err != nil || res.Family != FamilyQwen3 {
		t.Fatalf("guard disabled: %+v %v", res, err)
	}
	t.Setenv(EmbedMemGuardEnv, "")
	t.Setenv(EmbedMinFreeMBEnv, "0")
	availableMemoryFn = func() (uint64, bool) { return 1 << 20, true }
	if err := CheckModelMemory(filepath.Join(filepath.Dir(gp), Qwen3ModelFilename)); err != nil {
		t.Fatalf("4-byte file with 0 headroom and 1 MB free must pass: %v", err)
	}
}

func TestAvailableMemoryProbe(t *testing.T) {
	v, ok := availableMemory()
	if !ok {
		t.Skip("availability unknown on this OS")
	}
	if v == 0 {
		t.Fatal("available memory reported as 0")
	}
}

// ---- degrade, never crash ----

func TestQwen3EmbedPanicsBecomeErrors(t *testing.T) {
	var zero Qwen3Embedder
	if _, err := zero.Embed("hello"); err == nil {
		t.Fatal("embed on an unloaded instance must error")
	}
	if _, err := zero.EmbedBatch([]string{"a", "b", "c"}); err == nil {
		t.Fatal("batch on an unloaded instance must error (not crash a worker goroutine)")
	}
	broken := &Qwen3Embedder{tokenizer: &BPETokenizer{}} // tokenizer present, no weights
	if _, err := broken.Embed("hello"); err == nil {
		t.Fatal("embed with no weights must error")
	}
}

// ---- real model: Q8P→float, mmap→heap ----

func qwen3Cos(a, b []float32) float64 {
	var d, na, nb float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	return d / math.Sqrt(na*nb)
}

var hardenTexts = []string{"打开上周的销售报告", "How do I rebase a feature branch onto main?", "明天下午三点提醒我开会"}

func embedAll(t *testing.T, e *Qwen3Embedder) [][]float32 {
	t.Helper()
	out := make([][]float32, len(hardenTexts))
	for i, s := range hardenTexts {
		v, err := e.Embed(s)
		if err != nil {
			t.Fatal(err)
		}
		out[i] = v
	}
	return out
}

func TestQwen3Q8PFallsBackToFloat(t *testing.T) {
	p := qwen3TestModel(t)
	if !tensor.Q8PAvailable() {
		t.Skip("no Q8P kernel on this CPU: float is already the path (ISA gate)")
	}
	t.Setenv("MACLAW_QWEN3_Q8P", "0")
	ref, err := NewQwen3Embedder(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := embedAll(t, ref)
	ref.Close()
	t.Setenv("MACLAW_QWEN3_Q8P", "")

	// Healthy host: the self-check passes and Q8P is used.
	ok, err := NewQwen3Embedder(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !ok.q8p || ok.Q8PFallbackReason() != "" {
		t.Fatalf("q8p=%v reason=%q", ok.q8p, ok.Q8PFallbackReason())
	}
	ok.Close()

	for name, inject := range map[string]func(){
		"pack-nil": func() {
			n := 0
			qwen3PackQ8PFn = func(t *tensor.Q8Tensor, a *tensor.Q8PArena) *tensor.Q8Packed {
				if n++; n == 37 {
					return nil
				}
				return tensor.PackQ8PInto(t, a)
			}
		},
		"pack-panic": func() {
			qwen3PackQ8PFn = func(*tensor.Q8Tensor, *tensor.Q8PArena) *tensor.Q8Packed { panic("SIGILL-like") }
		},
		"self-check": func() {
			qwen3Q8PSelfCheckFn = func(*tensor.Q8Tensor, *tensor.Q8Packed) error { return errors.New("relative error 0.9") }
		},
	} {
		t.Run(name, func(t *testing.T) {
			oPack, oCheck := qwen3PackQ8PFn, qwen3Q8PSelfCheckFn
			defer func() { qwen3PackQ8PFn, qwen3Q8PSelfCheckFn = oPack, oCheck }()
			inject()
			e, err := NewQwen3Embedder(p, 0)
			if err != nil {
				t.Fatalf("load must not fail: %v", err)
			}
			defer e.Close()
			if e.q8p || e.Q8PFallbackReason() == "" || e.ComputeMode() != "float" {
				t.Fatalf("q8p=%v reason=%q mode=%s", e.q8p, e.Q8PFallbackReason(), e.ComputeMode())
			}
			got := embedAll(t, e)
			for i := range got {
				if c := qwen3Cos(got[i], want[i]); c < 0.99999 {
					t.Fatalf("%q: cos to float reference %.6f", hardenTexts[i], c)
				}
			}
		})
	}
}

func TestQwen3LoadsWhenMmapFails(t *testing.T) {
	p := qwen3TestModel(t)
	a, err := NewQwen3Embedder(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := embedAll(t, a)
	a.Close()
	t.Setenv(gguf.NoMmapEnv, "1") // same code path as a failed mapping (see gguf tests)
	b, err := NewQwen3Embedder(p, 0)
	if err != nil {
		t.Fatalf("heap fallback load: %v", err)
	}
	defer b.Close()
	if !b.mmap.Heap() {
		t.Fatal("expected heap-backed weights")
	}
	got := embedAll(t, b)
	for i := range got {
		if c := qwen3Cos(got[i], want[i]); c < 0.99999 {
			t.Fatalf("%q: cos %.6f", hardenTexts[i], c)
		}
	}
}
