package llm

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

type routeDeadlineKey struct{}
type streamCancelKey struct{}

// WithOwnedRouteDeadline records a deadline this process installed for one
// model route. Streaming reads may outlive it after SSE headers. An earlier
// host deadline is not recorded and still aborts the whole call.
func WithOwnedRouteDeadline(ctx context.Context, deadline time.Time) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, routeDeadlineKey{}, deadline)
}

// BindRouteStreamDeadline records an owned route deadline and the context
// whose cancellation must still abort the SSE body after that deadline.
// cancelParent is the context from before the route timeout was applied, so
// a stop or host deadline remains visible once the route timer has fired.
func BindRouteStreamDeadline(ctx context.Context, deadline time.Time, cancelParent context.Context) context.Context {
	ctx = WithOwnedRouteDeadline(ctx, deadline)
	if cancelParent != nil {
		ctx = context.WithValue(ctx, streamCancelKey{}, cancelParent)
	}
	return ctx
}

// OwnedRouteDeadline returns the deadline recorded by WithOwnedRouteDeadline.
func OwnedRouteDeadline(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Time{}, false
	}
	deadline, ok := ctx.Value(routeDeadlineKey{}).(time.Time)
	return deadline, ok && !deadline.IsZero()
}

// streamingRequestContext is the context for one SSE HTTP request.
//
// Before response headers, the call is canceled when the caller is canceled
// or when an owned route deadline expires. markHeaders returns false when
// that deadline already owns the request. After a true mark, the route
// deadline no longer cancels the body, and the body has no wall-clock cap.
// It runs until the model finishes, the SSE idle timeout, or caller
// cancellation. Cancelling a WithCancel wrapped around the route context
// aborts the body as well. A keepalive that keeps the idle clock fresh does
// not end it.
//
// Contexts without an owned route deadline are returned unchanged so a host
// deadline keeps its existing meaning. headerDeadline reports that the route
// timer canceled the call before headers; callers map that to
// context.DeadlineExceeded so a retry still sees a timeout.
func streamingRequestContext(parent context.Context) (context.Context, context.CancelFunc, func() bool, *atomic.Bool) {
	headerDeadline := &atomic.Bool{}
	if parent == nil {
		parent = context.Background()
	}
	routeDeadline, owned := OwnedRouteDeadline(parent)
	if !owned {
		return parent, func() {}, func() bool { return true }, headerDeadline
	}
	cancelCtx := parent
	if stored, _ := parent.Value(streamCancelKey{}).(context.Context); stored != nil {
		cancelCtx = stored
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	var mu sync.Mutex
	const (
		streamPhaseWaiting = iota
		streamPhaseHeaders
		streamPhaseAborted
	)
	phase := streamPhaseWaiting
	// claimTimeout wins the pre-header race against markHeaders. The loser
	// sees the phase the winner stored, so a confirmed SSE body is not
	// canceled by a timer that already lost.
	claimTimeout := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if phase != streamPhaseWaiting {
			return false
		}
		phase = streamPhaseAborted
		headerDeadline.Store(true)
		return true
	}
	var timer *time.Timer
	// markHeaders reports whether this call owns the SSE body. A route timeout
	// that already claimed the request leaves the body on that deadline.
	markHeaders := func() bool {
		mu.Lock()
		won := phase == streamPhaseWaiting
		if won {
			phase = streamPhaseHeaders
		}
		mu.Unlock()
		if won && timer != nil {
			timer.Stop()
		}
		return won
	}
	timer = time.AfterFunc(time.Until(routeDeadline), func() {
		if claimTimeout() {
			cancel()
		}
	})
	// cancelCtx outlives the route deadline when it is the pre-timeout parent.
	// A stop or host deadline still aborts the body. When it is the route
	// context itself, DeadlineExceeded is the route timer and is ignored once
	// headers have been marked.
	stopWatch := context.AfterFunc(cancelCtx, func() {
		err := cancelCtx.Err()
		if cancelCtx == parent && err == context.DeadlineExceeded {
			if claimTimeout() {
				cancel()
			}
			return
		}
		if err == context.DeadlineExceeded {
			claimTimeout()
		}
		cancel()
	})
	// A WithCancel between the route timeout and this call is how the scheduler
	// and a reasoning halt abort one request. That wrapper dies with
	// DeadlineExceeded when the route timer fires, which must not end a body
	// that already has headers. Any other cancel of the wrapper still does.
	var parentWatch func() bool
	if cancelCtx != parent {
		parentWatch = context.AfterFunc(parent, func() {
			if parent.Err() == context.DeadlineExceeded {
				if claimTimeout() {
					cancel()
				}
				return
			}
			cancel()
		})
	}
	var once sync.Once
	finish := func() {
		once.Do(func() {
			timer.Stop()
			stopWatch()
			if parentWatch != nil {
				parentWatch()
			}
			cancel()
		})
	}
	return ctx, finish, markHeaders, headerDeadline
}

// StreamingRequestContext is streamingRequestContext for transports that
// build the HTTP or websocket request outside this package. Before headers,
// an owned route deadline still cancels the call. After markHeaders reports
// true, that deadline no longer cancels the body. Caller stop and an earlier
// host deadline still do. A context without an owned route deadline is
// returned unchanged.
func StreamingRequestContext(parent context.Context) (context.Context, context.CancelFunc, func() bool, *atomic.Bool) {
	return streamingRequestContext(parent)
}

// NormalizeStreamSetupError maps a pre-header owned-route cancellation to
// context.DeadlineExceeded so a retry still sees a timeout.
func NormalizeStreamSetupError(headerDeadline *atomic.Bool, err error) error {
	return normalizeStreamSetupError(headerDeadline, err)
}

// normalizeStreamSetupError turns a pre-header route timeout into
// context.DeadlineExceeded. The detached request context reports
// context.Canceled, which callers would not retry.
func normalizeStreamSetupError(headerDeadline *atomic.Bool, err error) error {
	if err == nil || headerDeadline == nil || !headerDeadline.Load() {
		return err
	}
	// Only the cancellation we just caused is a route timeout. A connection
	// failure that races the timer keeps its own error.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return err
}
