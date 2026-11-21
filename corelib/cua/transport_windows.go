//go:build windows

package cua

import (
	"context"
	"fmt"
	"sync"
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
	h    windows.Handle
	mu   sync.Mutex
	buf  []byte
	done bool
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
// after the newline stay buffered for the next call.
func (p *pipeConn) readLine(ctx context.Context) ([]byte, error) {
	var acc []byte
	for {
		p.mu.Lock()
		if p.done {
			p.mu.Unlock()
			return nil, ErrNoDriver
		}
		if i := indexByte(acc, 10); i >= 0 {
			line := append([]byte(nil), acc[:i]...)
			p.buf = append(p.buf, acc[i+1:]...)
			p.mu.Unlock()
			return line, nil
		}
		p.mu.Unlock()
		chunk, err := p.readChunk(ctx)
		if err != nil {
			return nil, err
		}
		acc = append(acc, chunk...)
		if len(acc) > 16<<20 {
			return nil, fmt.Errorf("cua: response line over 16MiB")
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
		ch <- outcome{data: buf[:n]}
	}()
	select {
	case res := <-ch:
		return res.data, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// stop makes in-flight and future reads fail.
func (p *pipeConn) stop() {
	p.mu.Lock()
	p.done = true
	p.mu.Unlock()
}

// closeHandle releases the OS handle.
func (p *pipeConn) closeHandle() {
	procCloseHandle.Call(uintptr(p.h))
}
