package cua

import (
	"fmt"
	"io"
)

// readLineConn is the net.Conn sibling for unix / tcp transports. A
// net.Conn read is cancellable via SetDeadline, so no goroutine is needed.
func readLineConn(r io.Reader) ([]byte, error) {
	var out []byte
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if i := indexOfByte(out, '\n'); i >= 0 {
			return out[:i], nil
		}
		if err != nil {
			if err == io.EOF && len(out) > 0 {
				return out, nil
			}
			return out, err
		}
		if len(out) > 16<<20 {
			return nil, fmt.Errorf("response line over 16MiB")
		}
	}
}
