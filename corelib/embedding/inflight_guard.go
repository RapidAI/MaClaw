// corelib/embedding/inflight_guard.go — Close vs in-flight inference.
//
// The local embedders read their weights straight out of an mmap'd GGUF.
// Close used to unmap after taking only the sequential-path mutex, while
// EmbedConcurrent / EmbedBatch (anchor warmup, batch indexing) run without
// it: closing during a warmup unmapped pages a worker was still reading and
// crashed the whole process with SIGSEGV ("unexpected fault address", seen in
// TestComputerUseCalibrationWithRealModel when its warmup timed out under
// load). inflightGuard makes Close wait for every running call to finish and
// makes calls that start after Close fail with ErrEmbedderClosed, so the mmap
// is never released under a reader.
//
// acquire never blocks (unlike sync.RWMutex.RLock behind a waiting writer), so
// nested use — EmbedBatch → EmbedConcurrent, EmbedWithRole → Embed — cannot
// deadlock against a concurrent Close.
package embedding

import (
	"errors"
	"sync"
)

// ErrEmbedderClosed is returned by calls made after Close.
var ErrEmbedderClosed = errors.New("embedding: embedder closed")

type inflightGuard struct {
	mu     sync.Mutex
	cond   *sync.Cond
	n      int
	closed bool
}

// acquire registers an in-flight call; false once the guard is closed.
func (g *inflightGuard) acquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.n++
	return true
}

func (g *inflightGuard) release() {
	g.mu.Lock()
	g.n--
	if g.n == 0 && g.closed && g.cond != nil {
		g.cond.Broadcast()
	}
	g.mu.Unlock()
}

// close marks the guard closed and waits until no call is in flight.
// It returns false if the guard was already closed.
func (g *inflightGuard) close() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return false
	}
	g.closed = true
	if g.cond == nil {
		g.cond = sync.NewCond(&g.mu)
	}
	for g.n > 0 {
		g.cond.Wait()
	}
	return true
}

func (g *inflightGuard) isClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}
