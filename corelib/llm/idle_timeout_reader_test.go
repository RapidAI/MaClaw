package llm

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// stalledReadCloser delivers its payload immediately, then blocks until the
// test closes it — like a hub that accepted the request and went silent.
type stalledReadCloser struct {
	r         *strings.Reader
	block     chan struct{}
	closeOnce chan struct{}
}

func newStalledReadCloser(payload string) *stalledReadCloser {
	return &stalledReadCloser{
		r:         strings.NewReader(payload),
		block:     make(chan struct{}),
		closeOnce: make(chan struct{}),
	}
}

func (s *stalledReadCloser) Read(p []byte) (int, error) {
	if s.r.Len() > 0 {
		return s.r.Read(p)
	}
	<-s.closeOnce
	return 0, errors.New("read on closed body")
}

func (s *stalledReadCloser) Close() error {
	select {
	case <-s.closeOnce:
	default:
		close(s.closeOnce)
	}
	return nil
}

func TestIdleTimeoutReaderAbortsStalledStream(t *testing.T) {
	old := SSEIdleTimeout
	SSEIdleTimeout = 50 * time.Millisecond
	defer func() { SSEIdleTimeout = old }()

	body := newStalledReadCloser("data: {\"x\":1}\n\n")
	r := newIdleTimeoutReader(body, SSEIdleTimeout)
	defer r.Close()

	// The payload bytes arrive fine and reset the timer.
	buf := make([]byte, len("data: {\"x\":1}\n\n"))
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("first read: %v", err)
	}

	// Then the stream stalls: the next read must fail with an idle timeout.
	start := time.Now()
	_, err := r.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("stalled read unexpectedly succeeded")
	}
	if !IsSSEIdleTimeoutError(err) {
		t.Fatalf("err = %v, want idleTimeoutError", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("idle abort took %v, want ~%v", elapsed, SSEIdleTimeout)
	}
}

func TestIdleTimeoutReaderPassesThroughActiveStream(t *testing.T) {
	old := SSEIdleTimeout
	SSEIdleTimeout = time.Second
	defer func() { SSEIdleTimeout = old }()

	r := newIdleTimeoutReader(io.NopCloser(strings.NewReader("data: [DONE]\n\n")), SSEIdleTimeout)
	defer r.Close()

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(out) != "data: [DONE]\n\n" {
		t.Fatalf("payload = %q", out)
	}
}

// HTTPStatusError must satisfy the structural status-carrier interface that
// corelib/intent matches via errors.As (a direct import would cycle), so the
// classifier can classify 5xx endpoint failures.
func TestHTTPStatusErrorSatisfiesStatusCoderInterface(t *testing.T) {
	var coder interface{ HTTPStatusCode() int }
	err := &HTTPStatusError{StatusCode: 502}
	if !errors.As(err, &coder) {
		t.Fatal("HTTPStatusError does not satisfy the HTTPStatusCode() interface")
	}
	if coder.HTTPStatusCode() != 502 {
		t.Fatalf("HTTPStatusCode() = %d, want 502", coder.HTTPStatusCode())
	}
	if (&HTTPStatusError{}).HTTPStatusCode() != 0 {
		t.Fatal("zero HTTPStatusError must report status 0")
	}
}
