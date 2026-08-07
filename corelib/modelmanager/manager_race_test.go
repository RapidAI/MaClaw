package modelmanager

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestAutoUnload_NeverClosesInUseModel is a regression test for a race where
// the idle-unload timer callback (already fired, blocked on mu) closes the
// model right after a new Acquire returned it to a caller.
func TestAutoUnload_NeverClosesInUseModel(t *testing.T) {
	mgr := New(Config[*fakeModel]{
		Name:        "race",
		Load:        func() (*fakeModel, error) { return &fakeModel{}, nil },
		Close:       func(m *fakeModel) { atomic.StoreInt32(&m.closed, 1) },
		UnloadDelay: time.Millisecond,
	})
	for i := 0; i < 5000; i++ {
		m, done, err := mgr.Acquire()
		if err != nil {
			t.Fatal(err)
		}
		// Hold the model across the unload-delay boundary with jitter so a
		// pending timer callback can be mid-flight when Acquire runs.
		time.Sleep(time.Duration(rand.Intn(4000)) * time.Microsecond)
		if m.isClosed() {
			done()
			t.Fatalf("iteration %d: Acquire returned a model the idle timer closed while in use", i)
		}
		done()
	}
	mgr.Shutdown()
}

// TestAutoUnload_ConcurrentNoUseAfterClose hammers Acquire from many
// goroutines with a tiny unload delay; the idle timer must never close a
// model that an in-flight Acquire is holding.
func TestAutoUnload_ConcurrentNoUseAfterClose(t *testing.T) {
	mgr := New(Config[*fakeModel]{
		Name:        "race2",
		Load:        func() (*fakeModel, error) { return &fakeModel{}, nil },
		Close:       func(m *fakeModel) { atomic.StoreInt32(&m.closed, 1) },
		UnloadDelay: time.Millisecond,
	})
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				m, done, err := mgr.Acquire()
				if err != nil {
					t.Error(err)
					return
				}
				time.Sleep(1500 * time.Microsecond)
				if m.isClosed() {
					t.Error("model closed while in use")
					done()
					return
				}
				done()
			}
		}()
	}
	time.Sleep(5 * time.Second)
	close(stop)
	wg.Wait()
	mgr.Shutdown()
}
