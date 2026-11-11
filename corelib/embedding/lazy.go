package embedding

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/modelmanager"
)

// ErrPoolingUnsupported is returned by LazyEmbedder.EmbedPooledWithRole when
// the inner model does not implement PooledEmbedder; callers fall back to a
// plain role-scoped Embed the same way they do for prompt-less models.
var ErrPoolingUnsupported = errors.New("embedding: inner model does not support pooled embedding")

// LazyEmbedder is a stable embedding.Embedder facade whose weights are held
// by a modelmanager: the underlying model loads on first use, unloads after
// an idle delay, and transparently reloads on the next call. Consumers keep
// the facade forever, so embedder identity (sameEmbedder, pooled memos,
// attached heads) survives an idle unload/reload cycle.
//
// After Close the facade is permanently dead: Embed returns nil, nil like the
// noop embedder instead of reviving the model.
type LazyEmbedder struct {
	mm      *modelmanager.Manager[Embedder]
	closed  atomic.Bool
	dim     atomic.Int32
	modelID atomic.Value // string — mirrors the inner embedder's ModelID
	accel   atomic.Bool  // last ApplyAccel preference, re-applied on reload
}

// LazyEmbedderConfig configures a LazyEmbedder.
type LazyEmbedderConfig struct {
	// Name labels the manager in log lines (default "embedding").
	Name string
	// Load creates the real embedder. Called on the first use and again after
	// every idle unload. May return NoopEmbedder.
	Load func() (Embedder, error)
	// UnloadDelay is the idle timeout before the inner model is released.
	// Zero/negative keeps the model loaded until Close.
	UnloadDelay time.Duration
}

// NewLazyEmbedder creates the facade. Nothing is loaded until Warm or the
// first Embed-family call.
func NewLazyEmbedder(cfg LazyEmbedderConfig) *LazyEmbedder {
	name := cfg.Name
	if name == "" {
		name = "embedding"
	}
	l := &LazyEmbedder{}
	l.mm = modelmanager.New(modelmanager.Config[Embedder]{
		Name:        name,
		Load:        cfg.Load,
		Close:       func(e Embedder) { e.Close() },
		UnloadDelay: cfg.UnloadDelay,
	})
	return l
}

// Warm forces the inner model to load now and reports it for verification.
// The returned embedder is the manager's live instance: callers must not hold
// it past the idle timer (use the facade for inference). An error means the
// load failed, the same contract as calling the Load func directly.
func (l *LazyEmbedder) Warm() (Embedder, error) {
	if l.closed.Load() {
		return NoopEmbedder{}, nil
	}
	inner, done, err := l.mm.Acquire()
	if err != nil {
		return nil, err
	}
	done()
	if l.dropNoopInner(inner) {
		return NoopEmbedder{}, nil
	}
	l.noteInner(inner)
	return inner, nil
}

// Loaded reports whether the weights are currently in memory.
func (l *LazyEmbedder) Loaded() bool { return l.mm.Loaded() }

// Unload drops the weights early; the next call reloads them.
func (l *LazyEmbedder) Unload() { l.mm.Unload() }

func (l *LazyEmbedder) noteInner(inner Embedder) {
	if inner == nil || IsNoop(inner) {
		return
	}
	l.dim.Store(int32(inner.Dim()))
	if m, ok := inner.(interface{ ModelID() string }); ok {
		l.modelID.Store(m.ModelID())
	}
}

// dropNoopInner releases a noop result instead of caching it: the manager
// must retry the real load on the next call (the model file may appear after
// a background download), while callers still see the noop contract.
// Call only after the acquisition that produced inner has been released.
func (l *LazyEmbedder) dropNoopInner(inner Embedder) bool {
	if inner == nil || !IsNoop(inner) {
		return false
	}
	l.mm.Unload()
	return true
}

// acquire returns the live inner embedder for one call scope.
func (l *LazyEmbedder) acquire() (Embedder, func(), error) {
	if l.closed.Load() {
		return NoopEmbedder{}, func() {}, nil
	}
	inner, done, err := l.mm.Acquire()
	if err != nil {
		return nil, func() {}, err
	}
	if inner == nil || IsNoop(inner) {
		done()
		if inner != nil {
			l.mm.Unload()
		}
		return NoopEmbedder{}, func() {}, nil
	}
	l.noteInner(inner)
	return inner, done, nil
}

// Embed implements Embedder.
func (l *LazyEmbedder) Embed(text string) ([]float32, error) {
	inner, done, err := l.acquire()
	if err != nil {
		return nil, err
	}
	defer done()
	return inner.Embed(text)
}

// EmbedBatch implements Embedder.
func (l *LazyEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	inner, done, err := l.acquire()
	if err != nil {
		return nil, err
	}
	defer done()
	return inner.EmbedBatch(texts)
}

// EmbedConcurrent keeps the lock-free concurrent inference optimization
// reachable through the facade (tool.ConcurrentEmbedder); inners without it
// fall back to a plain Embed exactly like ConcurrentEmbedder-less models.
func (l *LazyEmbedder) EmbedConcurrent(text string) ([]float32, error) {
	inner, done, err := l.acquire()
	if err != nil {
		return nil, err
	}
	defer done()
	if ce, ok := inner.(interface {
		EmbedConcurrent(string) ([]float32, error)
	}); ok {
		return ce.EmbedConcurrent(text)
	}
	return inner.Embed(text)
}

// Dim implements Embedder without forcing a load: the live instance answers
// while loaded; afterwards the last observed width is returned (the model is
// pinned per installation, so it cannot drift across an unload/reload).
func (l *LazyEmbedder) Dim() int {
	if inner, ok := l.mm.Peek(); ok && inner != nil {
		return inner.Dim()
	}
	return int(l.dim.Load())
}

// Close implements Embedder. The facade stays non-noop for identity checks
// but every subsequent Embed-family call returns the noop result.
func (l *LazyEmbedder) Close() {
	if l.closed.Swap(true) {
		return
	}
	l.mm.Unload()
}

// EmbedWithRole implements RoleEmbedder, delegating when the inner model
// understands task prompts and degrading to a plain Embed otherwise — the
// same fallback EmbedAs applies to prompt-less embedders.
func (l *LazyEmbedder) EmbedWithRole(text string, role Role) ([]float32, error) {
	inner, done, err := l.acquire()
	if err != nil {
		return nil, err
	}
	defer done()
	if re, ok := inner.(RoleEmbedder); ok {
		return re.EmbedWithRole(text, role)
	}
	return inner.Embed(text)
}

// EmbedBatchWithRole implements RoleEmbedder.
func (l *LazyEmbedder) EmbedBatchWithRole(texts []string, role Role) ([][]float32, error) {
	inner, done, err := l.acquire()
	if err != nil {
		return nil, err
	}
	defer done()
	if re, ok := inner.(RoleEmbedder); ok {
		return re.EmbedBatchWithRole(texts, role)
	}
	return inner.EmbedBatch(texts)
}

// EmbedPooledWithRole implements PooledEmbedder for inners that support it.
// The caller's "an embedder may implement the interface and still fail"
// fallback covers inners without pooling.
func (l *LazyEmbedder) EmbedPooledWithRole(text string, role Role) (last, mean []float32, err error) {
	inner, done, err := l.acquire()
	if err != nil {
		return nil, nil, err
	}
	defer done()
	pe, ok := inner.(PooledEmbedder)
	if !ok {
		return nil, nil, ErrPoolingUnsupported
	}
	return pe.EmbedPooledWithRole(text, role)
}

// ModelID delegates to the inner embedder's ModelID when it has one; the last
// observed value is kept across an idle unload so vector-space tagging stays
// stable while the weights are out of memory.
func (l *LazyEmbedder) ModelID() string {
	if inner, ok := l.mm.Peek(); ok && inner != nil {
		if m, ok2 := inner.(interface{ ModelID() string }); ok2 {
			return m.ModelID()
		}
	}
	if v, ok := l.modelID.Load().(string); ok {
		return v
	}
	return ""
}

// Accel implements AccelEmbedder: while the model is in memory the live
// backend is reported; otherwise the process-wide probe result is used, the
// same answer a non-accelerated embedder gives.
func (l *LazyEmbedder) Accel() AccelInfo {
	if inner, ok := l.mm.Peek(); ok && inner != nil {
		if g, ok2 := inner.(AccelEmbedder); ok2 {
			return g.Accel()
		}
	}
	return CurrentAccelInfo()
}

// ApplyAccel implements AccelEmbedder. The preference is remembered and
// re-applied by the Load path after every idle reload.
func (l *LazyEmbedder) ApplyAccel(enabled bool) {
	l.accel.Store(enabled)
	if inner, ok := l.mm.Peek(); ok && inner != nil {
		if g, ok2 := inner.(AccelEmbedder); ok2 {
			g.ApplyAccel(enabled)
		}
	}
}

// AccelPreferred reports the last ApplyAccel value so a Load closure can
// re-apply it to a reloaded model.
func (l *LazyEmbedder) AccelPreferred() bool { return l.accel.Load() }

// ComputeMode reports the inner model's numeric path while it is loaded;
// "unloaded" while the weights are paged out (logs should not lie about the
// backend of a model that is not in memory).
func (l *LazyEmbedder) ComputeMode() string {
	if inner, ok := l.mm.Peek(); ok && inner != nil {
		if m, ok2 := inner.(interface{ ComputeMode() string }); ok2 {
			return m.ComputeMode()
		}
		return EmbedderFamily(inner)
	}
	return "unloaded"
}
