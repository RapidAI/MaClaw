package main

import "context"

// contextDone returns nil for an absent context, which disables the select
// case. It makes cancellation-aware wait loops explicit and nil-safe.
func contextDone(ctx context.Context) <-chan struct{} {
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}
