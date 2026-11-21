//go:build windows

package cua

import (
	"context"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// # Windows named pipe transport
//
// `cua-driver serve --socket <name>` creates the pipe instance on demand,
// so the client opens with OPEN_EXISTING. The handle is not overlapped: a
// synchronous ReadFile is the only form this daemon's pipe supports. Reads
// run in a goroutine whose result is selected against the context, so a
// caller can give up on a slow tool; close() sets done and closes the
// handle, which unblocks the abandoned read and lets its goroutine exit.

var (
	modkernel32     = windows.NewLazySystemDLL("kernel32.dll")
	procCreateFileW = modkernel32.NewProc("CreateFileW")
	procCloseHandle = modkernel32.NewProc("CloseHandle")
)

// dialPipe opens the pipe instance named by addr.
func dialPipe(addr string) (pipeTransport, error) {
	target := pipeTarget(addr)
	name := windows.StringToUTF16(target)
	ret, _, callErr := procCreateFileW.Call(
		uintptr(unsafe.Pointer(&name[0])),
		0x80000000|0x40000000, // GENERIC_READ|GENERIC_WRITE
		0x7,                   // share read/write/delete
		0,                     // no security attributes
		3,                     // OPEN_EXISTING: the daemon creates the instance
		0,                     // synchronous handle
		0)
	if ret == ^uintptr(0) {
		return nil, fmt.Errorf("cua: open pipe %s: %v", target, callErr)
	}
	return &pipeConn{h: windows.Handle(ret)}, nil
}

// pipeConn is one pipe instance, read one line at a time.
type pipeConn struct {
	h     windows.Handle
	lines lineBuffer
	close sync.Once
}

// write sends b in whole chunks.
func (p *pipeConn) write(b []byte) error {
	for len(b) > 0 {
		var n uint32
		if err := windows.WriteFile(p.h, b, &n, nil); err != nil {
			return fmt.Errorf("cua: pipe write: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("cua: pipe write made no progress")
		}
		b = b[n:]
	}
	return nil
}

// readLine returns the next newline-terminated line, bounded by ctx. Bytes
// after the newline stay in lines so the next call is not shifted.
func (p *pipeConn) readLine(ctx context.Context) ([]byte, error) {
	for {
		line, ok, err := p.lines.takeLine()
		if err != nil {
			return nil, err
		}
		if ok {
			return line, nil
		}
		chunk, err := p.readChunk(ctx)
		if len(chunk) > 0 {
			if appendErr := p.lines.append(chunk); appendErr != nil {
				return nil, appendErr
			}
		}
		if err != nil {
			// A synchronous ReadFile cannot be cancelled. Leaving it running
			// and reading again would overlap two ReadFile calls on one
			// handle, and any bytes it later returns would be the answer to
			// a request the caller already abandoned.
			p.stop()
			time.AfterFunc(200*time.Millisecond, p.closeHandle)
			return nil, err
		}
	}
}

// readChunk reads one chunk, or returns on ctx's deadline. The read runs
// in its own goroutine because a synchronous ReadFile cannot be cancelled.
func (p *pipeConn) readChunk(ctx context.Context) ([]byte, error) {
	type outcome struct {
		data []byte
		err  error
	}
	ch := make(chan outcome, 1)
	go func() {
		buf := make([]byte, 64*1024)
		var n uint32
		if err := windows.ReadFile(p.h, buf, &n, nil); err != nil {
			ch <- outcome{err: fmt.Errorf("cua: pipe read: %w", err)}
			return
		}
		if n == 0 {
			ch <- outcome{err: ErrNoDriver}
			return
		}
		// Copy before the goroutine returns: the buffer is on this stack.
		ch <- outcome{data: append([]byte(nil), buf[:n]...)}
	}()
	select {
	case res := <-ch:
		return res.data, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// stop makes in-flight and future reads fail and drops a partial frame.
func (p *pipeConn) stop() {
	p.lines.stop()
}

// closeHandle releases the OS handle. Once: cancel and Close can both ask.
func (p *pipeConn) closeHandle() {
	p.close.Do(func() {
		procCloseHandle.Call(uintptr(p.h))
	})
}
