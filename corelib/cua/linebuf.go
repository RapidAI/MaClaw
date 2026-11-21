package cua

import (
	"fmt"
	"sync"
)

// lineBuffer reassembles newline-delimited frames. A ReadFile can return
// more than one line; the tail stays here so the next call is not shifted
// onto a later frame.
type lineBuffer struct {
	mu   sync.Mutex
	buf  []byte
	done bool
}

// stop retires the buffer. A later takeLine fails, and any partial frame
// is dropped so it cannot be read as the next response.
func (b *lineBuffer) stop() {
	b.mu.Lock()
	b.done = true
	b.buf = nil
	b.mu.Unlock()
}

// append keeps chunk for the next takeLine. A stopped buffer drops it.
func (b *lineBuffer) append(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return ErrNoDriver
	}
	b.buf = append(b.buf, chunk...)
	if len(b.buf) > 16<<20 {
		return fmt.Errorf("cua: response line over 16MiB")
	}
	return nil
}

// takeLine returns one complete line. ok is false when more bytes are
// needed. A stopped buffer returns ErrNoDriver even if a line was buffered:
// those bytes belong to a request the caller already abandoned.
func (b *lineBuffer) takeLine() (line []byte, ok bool, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.done {
		return nil, false, ErrNoDriver
	}
	i := indexByte(b.buf, '\n')
	if i < 0 {
		return nil, false, nil
	}
	line = append([]byte(nil), b.buf[:i]...)
	b.buf = append([]byte(nil), b.buf[i+1:]...)
	return line, true, nil
}
