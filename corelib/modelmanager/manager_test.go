package modelmanager

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeModel struct {
	closed int32
}

func (m *fakeModel) isClosed() bool { return atomic.LoadInt32(&m.closed) != 0 }

func TestAcquire_LazyLoad(t *testing.T) {
	var loadCount int32
	mgr := New(Config[*fakeModel]{
		Name: "test",
		Load: func() (*fakeModel, error) {
			atomic.AddInt32(&loadCount, 1)
			return &fakeModel{}, nil
		},
		UnloadDelay: 0, // no auto-unload
	})

	if mgr.Loaded() {
		t.Fatal("should not be loaded before Acquire")
	}

	m, done, err := mgr.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	done()

	if m == nil {
		t.Fatal("model should not be nil")
	}
	if !mgr.Loaded() {
		t.Fatal("should be loaded after Acquire")
	}
	if atomic.LoadInt32(&loadCount) != 1 {
		t.Fatalf("expected 1 load, got %d", loadCount)
	}

	// Second Acquire should reuse, not reload.
	m2, done2, err := mgr.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	done2()
	if m2 != m {
		t.Fatal("second Acquire should return same instance")
	}
	if atomic.LoadInt32(&loadCount) != 1 {
		t.Fatalf("expected 1 load (reuse), got %d", loadCount)
	}
}

func TestAcquire_LoadError(t *testing.T) {
	mgr := New(Config[*fakeModel]{
		Name: "test",
		Load: func() (*fakeModel, error) {
			return nil, errors.New("disk on fire")
		},
	})

	_, done, err := mgr.Acquire()
	done()
	if err == nil {
		t.Fatal("expected error")
	}
	if mgr.Loaded() {
		t.Fatal("should not be loaded after failed Acquire")
	}
}

func TestAutoUnload(t *testing.T) {
	var closeCount int32
	mgr := New(Config[*fakeModel]{
		Name: "test",
		Load: func() (*fakeModel, error) { return &fakeModel{}, nil },
		Close: func(m *fakeModel) {
			atomic.StoreInt32(&m.closed, 1)
			atomic.AddInt32(&closeCount, 1)
		},
		UnloadDelay: 100 * time.Millisecond,
	})

	m, done, err := mgr.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	done() // starts unload timer

	if !mgr.Loaded() {
		t.Fatal("should still be loaded immediately after done()")
	}

	// Wait for auto-unload.
	time.Sleep(250 * time.Millisecond)

	if mgr.Loaded() {
		t.Fatal("should be unloaded after idle timeout")
	}
	if !m.isClosed() {
		t.Fatal("Close should have been called")
	}
	if atomic.LoadInt32(&closeCount) != 1 {
		t.Fatalf("expected 1 close, got %d", closeCount)
	}
}

func TestAutoUnload_ResetByAcquire(t *testing.T) {
	mgr := New(Config[*fakeModel]{
		Name: "test",
		Load: func() (*fakeModel, error) { return &fakeModel{}, nil },
		UnloadDelay: 200 * time.Millisecond,
	})

	_, done1, _ := mgr.Acquire()
	done1() // timer starts: T+0

	time.Sleep(100 * time.Millisecond) // T+100ms — timer at 100/200ms

	// Acquire again — should reset the timer.
	_, done2, _ := mgr.Acquire()
	done2() // timer restarts: T+100ms

	time.Sleep(150 * time.Millisecond) // T+250ms — 150ms since last done(), < 200ms

	if !mgr.Loaded() {
		t.Fatal("should still be loaded — timer was reset by second Acquire")
	}

	time.Sleep(100 * time.Millisecond) // T+350ms — 250ms since last done(), > 200ms

	if mgr.Loaded() {
		t.Fatal("should be unloaded after idle timeout")
	}
}

func TestManualUnload(t *testing.T) {
	var closeCount int32
	mgr := New(Config[*fakeModel]{
		Name: "test",
		Load: func() (*fakeModel, error) { return &fakeModel{}, nil },
		Close: func(m *fakeModel) {
			atomic.AddInt32(&closeCount, 1)
		},
		UnloadDelay: 0, // no auto-unload
	})

	_, done, _ := mgr.Acquire()
	done()

	mgr.Unload()
	if mgr.Loaded() {
		t.Fatal("should not be loaded after Unload")
	}
	if atomic.LoadInt32(&closeCount) != 1 {
		t.Fatalf("expected 1 close, got %d", closeCount)
	}

	// Unload again — should be idempotent.
	mgr.Unload()
	if atomic.LoadInt32(&closeCount) != 1 {
		t.Fatal("double Unload should not call Close twice")
	}
}

func TestReloadAfterUnload(t *testing.T) {
	var loadCount int32
	mgr := New(Config[*fakeModel]{
		Name: "test",
		Load: func() (*fakeModel, error) {
			atomic.AddInt32(&loadCount, 1)
			return &fakeModel{}, nil
		},
		UnloadDelay: 50 * time.Millisecond,
	})

	_, done1, _ := mgr.Acquire()
	done1()

	time.Sleep(100 * time.Millisecond) // auto-unload fires
	if mgr.Loaded() {
		t.Fatal("should be unloaded")
	}

	// Acquire again — should reload.
	_, done2, _ := mgr.Acquire()
	done2()

	if !mgr.Loaded() {
		t.Fatal("should be loaded after re-Acquire")
	}
	if atomic.LoadInt32(&loadCount) != 2 {
		t.Fatalf("expected 2 loads (initial + reload), got %d", loadCount)
	}
}

func TestConcurrentAcquire(t *testing.T) {
	var loadCount int32
	mgr := New(Config[*fakeModel]{
		Name: "test",
		Load: func() (*fakeModel, error) {
			atomic.AddInt32(&loadCount, 1)
			time.Sleep(50 * time.Millisecond) // simulate slow load
			return &fakeModel{}, nil
		},
		UnloadDelay: time.Second,
	})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, done, err := mgr.Acquire()
			if err != nil {
				t.Errorf("Acquire error: %v", err)
				return
			}
			defer done()
			if m == nil {
				t.Error("model is nil")
			}
		}()
	}
	wg.Wait()

	// Model should have been loaded exactly once despite 10 concurrent Acquires.
	if n := atomic.LoadInt32(&loadCount); n != 1 {
		t.Fatalf("expected 1 load, got %d", n)
	}
	mgr.Shutdown()
}

func TestNilClose(t *testing.T) {
	mgr := New(Config[*fakeModel]{
		Name:  "test",
		Load:  func() (*fakeModel, error) { return &fakeModel{}, nil },
		Close: nil, // no cleanup
	})

	_, done, _ := mgr.Acquire()
	done()

	// Should not panic.
	mgr.Unload()
	if mgr.Loaded() {
		t.Fatal("should not be loaded after Unload")
	}
}

func TestShutdownThenReacquire(t *testing.T) {
	var loadCount int32
	mgr := New(Config[*fakeModel]{
		Name: "test",
		Load: func() (*fakeModel, error) {
			atomic.AddInt32(&loadCount, 1)
			return &fakeModel{}, nil
		},
		UnloadDelay: time.Second,
	})

	_, done, _ := mgr.Acquire()
	done()
	mgr.Shutdown()

	if mgr.Loaded() {
		t.Fatal("should not be loaded after Shutdown")
	}

	// Acquire after Shutdown should reload (Shutdown is not permanent).
	_, done2, err := mgr.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	done2()

	if !mgr.Loaded() {
		t.Fatal("should be loaded after re-Acquire")
	}
	if atomic.LoadInt32(&loadCount) != 2 {
		t.Fatalf("expected 2 loads, got %d", loadCount)
	}
	mgr.Shutdown()
}
