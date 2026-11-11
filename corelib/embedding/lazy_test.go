package embedding

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lazyFakeEmbedder is a controllable Embedder double. A non-nil release
// channel makes Embed block until the channel closes (in-flight simulation).
type lazyFakeEmbedder struct {
	dim      int
	modelID  string
	release  chan struct{}
	embeds   atomic.Int32
	closed   atomic.Int32
	roleHits atomic.Int32
}

func (f *lazyFakeEmbedder) Embed(text string) ([]float32, error) {
	if f.release != nil {
		<-f.release
	}
	f.embeds.Add(1)
	return []float32{1, 2, 3}, nil
}

func (f *lazyFakeEmbedder) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{1}
	}
	return out, nil
}

func (f *lazyFakeEmbedder) Dim() int        { return f.dim }
func (f *lazyFakeEmbedder) Close()          { f.closed.Add(1) }
func (f *lazyFakeEmbedder) ModelID() string { return f.modelID }

// lazyFullEmbedder adds the optional interfaces the facade must preserve.
type lazyFullEmbedder struct {
	lazyFakeEmbedder
	pooledHits atomic.Int32
	accel      atomic.Bool
}

func (f *lazyFullEmbedder) EmbedWithRole(text string, role Role) ([]float32, error) {
	f.roleHits.Add(1)
	return f.Embed(text)
}

func (f *lazyFullEmbedder) EmbedBatchWithRole(texts []string, role Role) ([][]float32, error) {
	f.roleHits.Add(1)
	return f.EmbedBatch(texts)
}

func (f *lazyFullEmbedder) EmbedPooledWithRole(text string, role Role) ([]float32, []float32, error) {
	f.pooledHits.Add(1)
	return []float32{4}, []float32{5}, nil
}

func (f *lazyFullEmbedder) Accel() AccelInfo        { return AccelInfo{Backend: "test"} }
func (f *lazyFullEmbedder) ApplyAccel(enabled bool) { f.accel.Store(enabled) }

func waitForLazy(t *testing.T, d time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLazyEmbedderDefersLoadUntilFirstUse(t *testing.T) {
	var loads atomic.Int32
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load:        func() (Embedder, error) { loads.Add(1); return &lazyFakeEmbedder{dim: 1024}, nil },
		UnloadDelay: time.Hour,
	})
	defer l.Close()
	if l.Loaded() || loads.Load() != 0 {
		t.Fatal("facade loaded eagerly")
	}
	if _, err := l.Embed("x"); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 1 || !l.Loaded() {
		t.Fatalf("first use loads once: loads=%d loaded=%v", loads.Load(), l.Loaded())
	}
	if _, err := l.Embed("y"); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 1 {
		t.Fatalf("second use reloaded: loads=%d", loads.Load())
	}
}

func TestLazyEmbedderUnloadsAfterIdleAndReloads(t *testing.T) {
	var loads atomic.Int32
	var inner *lazyFakeEmbedder
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load: func() (Embedder, error) {
			loads.Add(1)
			inner = &lazyFakeEmbedder{dim: 1024}
			return inner, nil
		},
		UnloadDelay: 40 * time.Millisecond,
	})
	defer l.Close()
	if _, err := l.Embed("x"); err != nil {
		t.Fatal(err)
	}
	waitForLazy(t, 3*time.Second, func() bool { return !l.Loaded() }, "idle unload")
	if inner.closed.Load() != 1 {
		t.Fatalf("unload did not close inner: %d", inner.closed.Load())
	}
	if _, err := l.Embed("y"); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 2 {
		t.Fatalf("reload after idle unload: loads=%d", loads.Load())
	}
}

func TestLazyEmbedderDoesNotCacheNoop(t *testing.T) {
	var loads atomic.Int32
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load: func() (Embedder, error) {
			loads.Add(1)
			return NoopEmbedder{}, nil
		},
		UnloadDelay: time.Hour,
	})
	defer l.Close()
	if v, err := l.Embed("x"); v != nil || err != nil {
		t.Fatalf("noop embed = %v, %v", v, err)
	}
	if l.Loaded() {
		t.Fatal("noop result was cached as loaded")
	}
	if _, err := l.Embed("y"); err != nil {
		t.Fatal(err)
	}
	if loads.Load() != 2 {
		t.Fatalf("noop must retry the real load: loads=%d", loads.Load())
	}
}

func TestLazyEmbedderCloseIsPermanent(t *testing.T) {
	var loads atomic.Int32
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load:        func() (Embedder, error) { loads.Add(1); return &lazyFakeEmbedder{dim: 1024}, nil },
		UnloadDelay: time.Hour,
	})
	if _, err := l.Embed("x"); err != nil {
		t.Fatal(err)
	}
	l.Close()
	l.Close()
	if v, err := l.Embed("y"); v != nil || err != nil {
		t.Fatalf("post-close embed = %v, %v", v, err)
	}
	if loads.Load() != 1 {
		t.Fatalf("post-close call reloaded: loads=%d", loads.Load())
	}
	if w, err := l.Warm(); err != nil || !IsNoop(w) {
		t.Fatalf("post-close warm = %T, %v", w, err)
	}
}

func TestLazyEmbedderPreservesOptionalInterfaces(t *testing.T) {
	var l Embedder = NewLazyEmbedder(LazyEmbedderConfig{
		Load:        func() (Embedder, error) { return &lazyFullEmbedder{lazyFakeEmbedder: lazyFakeEmbedder{dim: 4}}, nil },
		UnloadDelay: time.Hour,
	})
	defer l.Close()
	if _, ok := l.(RoleEmbedder); !ok {
		t.Fatal("facade does not implement RoleEmbedder")
	}
	if _, ok := l.(PooledEmbedder); !ok {
		t.Fatal("facade does not implement PooledEmbedder")
	}
	if _, ok := l.(AccelEmbedder); !ok {
		t.Fatal("facade does not implement AccelEmbedder")
	}
	re := l.(RoleEmbedder)
	if _, err := re.EmbedWithRole("x", RoleQuery); err != nil {
		t.Fatal(err)
	}
	pe := l.(PooledEmbedder)
	last, mean, err := pe.EmbedPooledWithRole("x", RoleQuery)
	if err != nil || len(last) != 1 || len(mean) != 1 {
		t.Fatalf("pooled = %v %v %v", last, mean, err)
	}
	ae := l.(AccelEmbedder)
	ae.ApplyAccel(true)
	if got := ae.Accel().Backend; got != "test" {
		t.Fatalf("accel backend = %q", got)
	}
}

func TestLazyEmbedderPooledFallsBack(t *testing.T) {
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load:        func() (Embedder, error) { return &lazyFakeEmbedder{dim: 4}, nil },
		UnloadDelay: time.Hour,
	})
	defer l.Close()
	if _, _, err := l.EmbedPooledWithRole("x", RoleQuery); !errors.Is(err, ErrPoolingUnsupported) {
		t.Fatalf("pooled err = %v", err)
	}
	// A non-pooled inner still answers role-scoped calls with a plain Embed.
	if v, err := l.EmbedWithRole("x", RoleQuery); err != nil || len(v) != 3 {
		t.Fatalf("role fallback = %v, %v", v, err)
	}
}

func TestLazyEmbedderUnloadWaitsForInflight(t *testing.T) {
	release := make(chan struct{})
	inner := &lazyFakeEmbedder{dim: 4, release: release}
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load:        func() (Embedder, error) { return inner, nil },
		UnloadDelay: time.Hour,
	})
	defer l.Close()
	done := make(chan error, 1)
	go func() {
		v, err := l.Embed("x")
		if len(v) == 0 {
			done <- errors.New("empty vector")
			return
		}
		done <- err
	}()
	waitForLazy(t, 3*time.Second, func() bool { return l.Loaded() }, "load")
	l.Unload()
	// The acquisition is still in flight: the inner must not be closed.
	if inner.closed.Load() != 0 {
		t.Fatal("in-flight embedder was closed")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitForLazy(t, 3*time.Second, func() bool { return inner.closed.Load() == 1 }, "deferred close")
}

func TestLazyEmbedderReportsInnerSpace(t *testing.T) {
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load: func() (Embedder, error) {
			return &lazyFakeEmbedder{dim: 1024, modelID: "qwen3-instruct-v1:1024:test"}, nil
		},
		UnloadDelay: 40 * time.Millisecond,
	})
	defer l.Close()
	if _, err := l.Embed("x"); err != nil {
		t.Fatal(err)
	}
	if EmbedderFamily(l) != FamilyQwen3 {
		t.Fatalf("family = %q", EmbedderFamily(l))
	}
	waitForLazy(t, 3*time.Second, func() bool { return !l.Loaded() }, "idle unload")
	// Space identity survives the unload — calibrations and head checks must
	// keep answering for the model the facade will reload.
	if EmbedderFamily(l) != FamilyQwen3 {
		t.Fatalf("family after unload = %q", EmbedderFamily(l))
	}
	if l.Dim() != 1024 {
		t.Fatalf("dim after unload = %d", l.Dim())
	}
}

func TestLazyEmbedderWarm(t *testing.T) {
	var loads atomic.Int32
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load:        func() (Embedder, error) { loads.Add(1); return &lazyFakeEmbedder{dim: 4}, nil },
		UnloadDelay: time.Hour,
	})
	defer l.Close()
	inner, err := l.Warm()
	if err != nil {
		t.Fatal(err)
	}
	if inner == nil || IsNoop(inner) || !l.Loaded() {
		t.Fatalf("warm = %T loaded=%v", inner, l.Loaded())
	}
	l2 := NewLazyEmbedder(LazyEmbedderConfig{
		Load:        func() (Embedder, error) { return NoopEmbedder{}, nil },
		UnloadDelay: time.Hour,
	})
	defer l2.Close()
	w, err := l2.Warm()
	if err != nil || !IsNoop(w) {
		t.Fatalf("noop warm = %T, %v", w, err)
	}
	if l2.Loaded() {
		t.Fatal("noop warm left the facade loaded")
	}
}

func TestLazyEmbedderConcurrentUse(t *testing.T) {
	l := NewLazyEmbedder(LazyEmbedderConfig{
		Load:        func() (Embedder, error) { return &lazyFakeEmbedder{dim: 4}, nil },
		UnloadDelay: 5 * time.Millisecond,
	})
	defer l.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				if _, err := l.Embed("x"); err != nil {
					t.Error(err)
					return
				}
				if j%7 == 0 {
					l.Unload()
				}
			}
		}()
	}
	wg.Wait()
}
