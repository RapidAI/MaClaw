//go:build !windows

package cua

import "fmt"

// dialPipe returns an error on non-Windows platforms: the named-pipe
// transport is Windows-only. On Linux a caller should use a unix:// or
// host:port socket address instead, which goes through the net.Conn path.
func dialPipe(addr string) (pipeTransport, error) {
	return nil, fmt.Errorf("cua: named-pipe transport is not available on this platform; use a unix:// or host:port socket address")
}
