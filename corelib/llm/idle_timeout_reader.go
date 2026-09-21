package llm

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// SSEIdleTimeout bounds how long the corelib stream path waits between bytes
// from the server before treating the connection as stalled and aborting it.
// It mirrors guiapp's guiSSEIdleTimeout (llm_stream.go): reasoning models may
// legitimately pause for minutes before the first token, so the bound is
// generous, but a connection that delivers nothing at all for longer than
// this will not recover on its own — without the bound it hangs until the OS
// TCP timeout (many minutes), blocking the whole agent loop. Exported as a
// var so tests can shrink it (package tests must not run in parallel while
// overriding it).
//
// Scope: the OpenAI-compatible HTTP stream only. The Anthropic SDK stream
// (anthropic_sdk.go) manages its own reads and has no idle bound; production
// configs use the OpenAI wire, and adding one there needs SDK-level event
// observation rather than a body wrapper.
var SSEIdleTimeout = 4 * time.Minute

// idleTimeoutReader aborts reads on body when no byte arrives within timeout.
// Every successful read signals the supervisor goroutine to push the deadline
// forward; expiry closes the body, which unblocks an in-flight Read with an
// error. Close is idempotent and stops the supervisor, so a normally-completed
// stream never fires the timeout.
//
// The supervisor design (instead of timer.Reset on an AfterFunc) avoids a
// real race: Reset cannot unqueue a callback that is already due, so a byte
// arriving exactly at timer expiry could be followed by the stale callback
// closing a live stream.
type idleTimeoutReader struct {
	body    io.ReadCloser
	timeout time.Duration

	// resetCh receives a token per successful read; the supervisor drains it
	// to reschedule the deadline. Buffered with drop-on-full so a blocked
	// supervisor can never deadlock a reader.
	resetCh chan struct{}
	// closeCh closes when the reader is closed, ending the supervisor.
	closeCh chan struct{}
	once    sync.Once
	// timedOut marks that the supervisor aborted the stream; Read translates
	// the resulting body error into an idleTimeoutError.
	timedOut atomic.Bool
}

func newIdleTimeoutReader(body io.ReadCloser, timeout time.Duration) *idleTimeoutReader {
	r := &idleTimeoutReader{
		body:    body,
		timeout: timeout,
		resetCh: make(chan struct{}, 1),
		closeCh: make(chan struct{}),
	}
	go r.supervise()
	return r
}

func (r *idleTimeoutReader) supervise() {
	timer := time.NewTimer(r.timeout)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			// No bytes within the window: abort the stream. Closing the body
			// unblocks the scanner's in-flight Read; Read then wraps the
			// resulting error so callers can tell a stalled-stream abort from
			// an ordinary transport failure.
			if r.timedOut.CompareAndSwap(false, true) {
				_ = r.body.Close()
			}
			return
		case <-r.resetCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(r.timeout)
		case <-r.closeCh:
			return
		}
	}
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	n, err := r.body.Read(p)
	if n > 0 {
		select {
		case r.resetCh <- struct{}{}:
		default:
		}
	}
	if err != nil && r.timedOut.Load() {
		return n, &idleTimeoutError{timeout: r.timeout, cause: err}
	}
	return n, err
}

func (r *idleTimeoutReader) Close() error {
	r.once.Do(func() { close(r.closeCh) })
	return r.body.Close()
}

// idleTimeoutError marks a stream read aborted by the idle timer, so callers
// can distinguish "the server stalled" from an ordinary transport failure.
type idleTimeoutError struct {
	timeout time.Duration
	cause   error
}

func (e *idleTimeoutError) Error() string {
	return fmt.Sprintf("llm: SSE stream idle timeout (%v): no data received: %v", e.timeout, e.cause)
}

func (e *idleTimeoutError) Unwrap() error { return e.cause }

// IsSSEIdleTimeoutError reports whether err is a stream abort produced by
// SSEIdleTimeout.
func IsSSEIdleTimeoutError(err error) bool {
	var ite *idleTimeoutError
	return errors.As(err, &ite)
}
