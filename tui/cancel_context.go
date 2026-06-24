package main

import (
	"context"
	"sync"
	"time"
)

func contextFromCancelPoll(isCancelled func() bool) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var closeOnce sync.Once

	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if isCancelled != nil && isCancelled() {
					cancel()
					return
				}
			}
		}
	}()

	return ctx, func() {
		closeOnce.Do(func() {
			close(done)
		})
		cancel()
	}
}
