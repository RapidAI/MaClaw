package cua

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// scriptedPipe is an in-memory pipeTransport for the runtime's lock and
// liveness paths. It is not a Windows pipe.
type scriptedPipe struct {
	writeErr error
	reads    []pipeRead
	stopped  bool
	closed   bool
}

type pipeRead struct {
	line []byte
	err  error
}

func (p *scriptedPipe) write([]byte) error { return p.writeErr }
func (p *scriptedPipe) readLine(context.Context) ([]byte, error) {
	if len(p.reads) == 0 {
		return nil, errors.New("no frame")
	}
	next := p.reads[0]
	p.reads = p.reads[1:]
	return next.line, next.err
}
func (p *scriptedPipe) stop()        { p.stopped = true }
func (p *scriptedPipe) closeHandle() { p.closed = true }

func TestLivenessRejectsReadFailure(t *testing.T) {
	rt := &runtime{pipe: &scriptedPipe{reads: []pipeRead{{err: errors.New("broken pipe")}}}}
	err := rt.liveness(context.Background())
	if err == nil {
		t.Fatal("a read failure must not pass liveness")
	}
}

func TestLivenessRejectsWriteFailure(t *testing.T) {
	rt := &runtime{pipe: &scriptedPipe{writeErr: errors.New("pipe closed")}}
	if err := rt.liveness(context.Background()); err == nil {
		t.Fatal("a write failure must not pass liveness")
	}
}

func TestLivenessAcceptsUnknownMethodFrame(t *testing.T) {
	rt := &runtime{pipe: &scriptedPipe{reads: []pipeRead{{
		line: []byte(`{"ok":false,"error":"Unknown method: tools/list"}`),
	}}}}
	if err := rt.liveness(context.Background()); err != nil {
		t.Fatalf("a protocol error is a live daemon, got %v", err)
	}
}

func TestLivenessRejectsNonJSON(t *testing.T) {
	rt := &runtime{pipe: &scriptedPipe{reads: []pipeRead{{line: []byte("not json")}}}}
	if err := rt.liveness(context.Background()); err == nil {
		t.Fatal("a non-protocol frame must not pass liveness")
	}
}

func TestReadResponseSkipsNotification(t *testing.T) {
	rt := &runtime{pipe: &scriptedPipe{reads: []pipeRead{
		{line: []byte(`{"jsonrpc":"2.0","method":"progress","params":{"n":1}}`)},
		{line: []byte(`{"ok":true,"result":{"content":[{"type":"text","text":"ready"}]}}`)},
	}}}
	raw, err := rt.call(context.Background(), "list_windows", nil)
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	content, structured, isError, err := decodeToolResult(raw)
	if err != nil || isError || len(content) != 1 || content[0].Text != "ready" || structured != nil {
		t.Fatalf("decoded %+v structured=%v isError=%v err=%v", content, structured, isError, err)
	}
}

func TestReadResponseRejectsMismatchedID(t *testing.T) {
	rt := &runtime{pipe: &scriptedPipe{reads: []pipeRead{
		{line: []byte(`{"jsonrpc":"2.0","id":99,"result":{"ok":true}}`)},
	}}}
	_, err := rt.call(context.Background(), "list_windows", nil)
	if err == nil || !strings.Contains(err.Error(), "does not answer") {
		t.Fatalf("mismatched id must fail, got %v", err)
	}
	if !rt.closed.Load() {
		t.Fatal("a mismatched id must retire the connection")
	}
}

func TestDecodeToolResultRejectsOKFalseWithoutError(t *testing.T) {
	_, _, _, err := decodeToolResult([]byte(`{"ok":false}`))
	if err == nil {
		t.Fatal("ok:false must not decode as a successful tool value")
	}
}

func TestLineBufferKeepsTailOfCoalescedRead(t *testing.T) {
	var b lineBuffer
	if err := b.append([]byte("{\"id\":1}\n{\"id\":2}\n")); err != nil {
		t.Fatal(err)
	}
	first, ok, err := b.takeLine()
	if err != nil || !ok || string(first) != `{"id":1}` {
		t.Fatalf("first = %q ok=%v err=%v", first, ok, err)
	}
	second, ok, err := b.takeLine()
	if err != nil || !ok || string(second) != `{"id":2}` {
		t.Fatalf("second = %q ok=%v err=%v", second, ok, err)
	}
}

func TestLineBufferStopDropsAbandonedFrame(t *testing.T) {
	var b lineBuffer
	if err := b.append([]byte("{\"id\":1}\npartial")); err != nil {
		t.Fatal(err)
	}
	b.stop()
	if _, _, err := b.takeLine(); err != ErrNoDriver {
		t.Fatalf("stopped buffer must not yield an abandoned frame, got %v", err)
	}
}

func TestCloseDoesNotWaitForInFlightCall(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	rt := &runtime{pipe: &scriptedPipe{}}
	go func() {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		close(started)
		<-release
	}()
	<-started
	done := make(chan struct{})
	go func() {
		rt.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close blocked behind the in-flight call lock")
	}
	close(release)
	if !rt.closed.Load() {
		t.Fatal("close did not retire the runtime")
	}
}

