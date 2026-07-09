package structureddata

import (
	"context"
	"sync"
	"time"
)

// WriteBatcher serializes concurrent write operations through a single
// background goroutine, eliminating mutex contention and goroutine scheduling
// overhead between writes.
//
// Instead of N goroutines competing for sync.Mutex (thundering herd on unlock),
// the batcher collects pending writes into a FIFO channel and executes them
// sequentially in a dedicated goroutine. This gives:
//   - Zero mutex contention (single consumer, no Lock/Unlock overhead)
//   - Locality benefit (back-to-back writes on same connection, warm CPU cache)
//   - Backpressure (channel full = caller blocks until slot available)
//
// Each fn still executes its own transaction internally (CreateRecord's BeginTx).
// The batcher does NOT wrap multiple ops in a single tx (that requires
// refactoring all write methods to accept *sql.Tx — future optimization).
type WriteBatcher struct {
	store    *SQLiteStore
	pending  chan *batchedWrite
	stopOnce sync.Once
	stopCh   chan struct{}
	doneCh   chan struct{}
}

type batchedWrite struct {
	fn     func(ctx context.Context, store *SQLiteStore) error
	ctx    context.Context
	result chan error
}

// NewWriteBatcher creates a write batcher.
//   - queueSize: capacity of the pending operations channel (backpressure threshold)
func NewWriteBatcher(store *SQLiteStore, queueSize int, _ time.Duration) *WriteBatcher {
	if queueSize <= 0 {
		queueSize = 256
	}
	wb := &WriteBatcher{
		store:   store,
		pending: make(chan *batchedWrite, queueSize),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go wb.loop()
	return wb
}

// Submit enqueues a write operation and blocks until it completes.
// The fn is executed in the batcher's dedicated goroutine (serialized, no mutex).
func (wb *WriteBatcher) Submit(ctx context.Context, fn func(ctx context.Context, store *SQLiteStore) error) error {
	op := &batchedWrite{
		fn:     fn,
		ctx:    ctx,
		result: make(chan error, 1),
	}
	select {
	case wb.pending <- op:
	case <-wb.stopCh:
		// Batcher stopped, execute directly.
		return fn(ctx, wb.store)
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-op.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop gracefully shuts down the batcher, draining any pending writes.
func (wb *WriteBatcher) Stop() {
	wb.stopOnce.Do(func() {
		close(wb.stopCh)
		<-wb.doneCh
	})
}

func (wb *WriteBatcher) loop() {
	defer close(wb.doneCh)
	for {
		select {
		case op := <-wb.pending:
			// Execute sequentially — each op has its own internal transaction.
			op.result <- op.fn(op.ctx, wb.store)
		case <-wb.stopCh:
			// Drain remaining pending ops.
			wb.drain()
			return
		}
	}
}

func (wb *WriteBatcher) drain() {
	for {
		select {
		case op := <-wb.pending:
			op.result <- op.fn(op.ctx, wb.store)
		default:
			return
		}
	}
}
