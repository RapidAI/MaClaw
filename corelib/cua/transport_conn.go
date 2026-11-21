package cua

import (
	"context"
	"io"
	"strings"
	"time"
)

// connReader is a stream that can hand bytes back. net.Conn does not, so
// the runtime keeps one of these per connection.
type connReader struct {
	r   io.Reader
	buf []byte
}

// readLineConn reads one newline-terminated line. Bytes after that newline
// stay buffered: a socket Read can return two frames, and dropping the tail
// shifts every later response onto the wrong request.
func readLineConn(ctx context.Context, cr *connReader) ([]byte, error) {
	if cr == nil || cr.r == nil {
		return nil, ErrNoDriver
	}
	if setter, ok := cr.r.(interface{ SetReadDeadline(time.Time) error }); ok {
		if dl, ok := ctx.Deadline(); ok {
			_ = setter.SetReadDeadline(dl)
		} else {
			_ = setter.SetReadDeadline(time.Time{})
		}
	}
	buf := make([]byte, 32*1024)
	for {
		if i := strings.IndexByte(string(cr.buf), '\n'); i >= 0 {
			line := append([]byte(nil), cr.buf[:i]...)
			cr.buf = append([]byte(nil), cr.buf[i+1:]...)
			return line, nil
		}
		n, err := cr.r.Read(buf)
		if n > 0 {
			cr.buf = append(cr.buf, buf[:n]...)
			if len(cr.buf) > 16<<20 {
				return nil, ErrNoDriver
			}
		}
		if err != nil {
			if err == io.EOF && len(cr.buf) > 0 {
				line := append([]byte(nil), cr.buf...)
				cr.buf = nil
				return line, nil
			}
			return nil, err
		}
	}
}

// indexOfByte returns the first index of c in b, or -1.
func indexOfByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}
