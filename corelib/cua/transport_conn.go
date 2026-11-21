package cua

import (
	"context"
	"io"
	"strings"
	"time"
)

// readLineConn reads one newline-terminated line from a stream. A net.Conn
// read is cancellable through its deadline, so no goroutine is needed.
// Unix and tcp sockets are how desktopd inside a container reaches its
// cua-driver; Windows uses the named pipe in transport_windows.go.
func readLineConn(ctx context.Context, r io.Reader) ([]byte, error) {
	if setter, ok := r.(interface{ SetReadDeadline(time.Time) error }); ok {
		if dl, ok := ctx.Deadline(); ok {
			_ = setter.SetReadDeadline(dl)
		}
	}
	var out []byte
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if i := strings.IndexByte(string(out), '\n'); i >= 0 {
			return out[:i], nil
		}
		if err != nil {
			if err == io.EOF && len(out) > 0 {
				return out, nil
			}
			return out, err
		}
		if len(out) > 16<<20 {
			return nil, ErrNoDriver
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
