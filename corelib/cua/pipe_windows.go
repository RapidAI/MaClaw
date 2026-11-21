package cua

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 handles used by the pipe transport.
var (
	k32CreateFileW = windows.NewLazySystemDLL("kernel32.dll")
	procCreateFileW = k32CreateFileW.NewProc("CreateFileW")
	procWriteFile   = k32CreateFileW.NewProc("WriteFile")
	procReadFile    = k32CreateFileW.NewProc("ReadFile")
	procCloseHandle = k32CreateFileW.NewProc("CloseHandle")
)

// pipeConn is one Windows named-pipe instance. The daemon creates one
// instance per client, so there is no instance-pool bookkeeping here.
//
// A blocking Win32 read cannot be cancelled from another goroutine, so
// every read runs in a goroutine. The caller can time out and close the
// handle; the abandoned read then unblocks on EOF, and the goroutine
// exits. A new read starts a new goroutine.
type pipeConn struct {
	h    uintptr
	mu   sync.Mutex
	stop chan struct{} // closed by stopSignal; observed by abandoned reads
	once sync.Once
}

// write sends b in whole chunks; a partial write is an error - the reader
// reassembles by line, and splitting a JSON line across two reads would
// corrupt the framing.
func (p *pipeConn) write(b []byte) error {
	for len(b) > 0 {
		var n uint32
		ret, _, _ := procWriteFile.Call(
			p.h, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)),
			uintptr(unsafe.Pointer(&n)), 0)
		if ret == 0 {
			return fmt.Errorf("WriteFile")
		}
		if n == 0 {
			// The buffer is full; retry without progress.
			continue
		}
		b = b[n:]
	}
	return nil
}

// readLine blocks until a full line (or end of stream) is available.
// Each call starts a fresh read goroutine for the single line it wants;
// when the handle dies, the goroutine exits and the next call sees
// ErrNoDriver.
func (p *pipeConn) readLine() ([]byte, error) {
	p.mu.Lock()
	if p.stop == nil {
		p.stop = make(chan struct{})
	}
	stop := p.stop
	p.mu.Unlock()

	type outcome struct {
		line []byte
		err  error
	}
	ch := make(chan outcome, 1)
	go func() {
		buf := make([]byte, 1<<20)
		var out []byte
		for {
			select {
			case <-stop:
				return
			default:
			}
			var n uint32
			ret, _, _ := procReadFile.Call(
				p.h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
				uintptr(unsafe.Pointer(&n)), 0)
			if ret == 0 {
				if len(out) > 0 {
					ch <- outcome{line: out}
				} else {
					ch <- outcome{err: ErrNoDriver}
				}
				return
			}
			out = append(out, buf[:n]...)
			if i := indexOfByte(out, '\n'); i >= 0 {
				ch <- outcome{line: out[:i]}
				return
			}
			if len(out) > 16<<20 {
				ch <- outcome{err: fmt.Errorf("response line over 16MiB")}
				return
			}
		}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		return res.line, nil
	case <-stop:
		return nil, ErrNoDriver
	}
}

// stopSignal makes every in-flight and future read return ErrNoDriver.
// Called by close().
func (p *pipeConn) stopSignal() {
	p.once.Do(func() {
		p.mu.Lock()
		if p.stop == nil {
			p.stop = make(chan struct{})
		}
		close(p.stop)
		p.mu.Unlock()
	})
}

func indexOfByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}
