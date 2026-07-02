package main

import "context"

// contextFromCancelCh returns a context that is cancelled when cancelCh is
// closed. This is a zero-CPU-overhead mechanism for propagating cancellation
// — no polling goroutine is spawned; the internal goroutine blocks on select
// until either the cancel channel closes or the returned context is cancelled
// by the caller (which terminates the goroutine).
//
// If cancelCh is nil, the returned context can only be cancelled by the caller
// invoking the returned CancelFunc (no external cancel signal).
func contextFromCancelCh(cancelCh <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	if cancelCh != nil {
		go func() {
			select {
			case <-cancelCh:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	return ctx, cancel
}
