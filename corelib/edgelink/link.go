// Package edgelink is the byte protocol on one Bot-node tunnel.
// The node dials Hub. Hub opens a yamux stream per request and writes the
// target name ("desktopd" or "srv") as one line. The node copies that
// stream to the local address configured for that target and to nothing else.
package edgelink

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

const (
	TargetDesktopd = "desktopd"
	TargetSrv      = "srv"
)

var ErrTarget = errors.New("edge target refused")

// AllowedTarget is a target the node is willing to forward.
func AllowedTarget(target string) bool {
	switch strings.TrimSpace(target) {
	case TargetDesktopd, TargetSrv:
		return true
	default:
		return false
	}
}

// WriteTarget marks one Hub-opened stream. It must be the first bytes.
func WriteTarget(w io.Writer, target string) error {
	target = strings.TrimSpace(target)
	if !AllowedTarget(target) {
		return fmt.Errorf("%w: %s", ErrTarget, target)
	}
	_, err := io.WriteString(w, target+"\n")
	return err
}

// prefixConn puts bytes already buffered while reading the target line back
// in front of conn, so the HTTP server sees a normal request.
type prefixConn struct {
	net.Conn
	pre io.Reader
}

func (c prefixConn) Read(b []byte) (int, error) {
	return c.pre.Read(b)
}

// ReadTargetConn reads the target and returns a conn whose reads start at the
// HTTP bytes that followed it.
func ReadTargetConn(conn net.Conn) (string, net.Conn, error) {
	br := bufio.NewReader(conn)
	var buf []byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			return "", nil, err
		}
		if b == '\n' {
			break
		}
		if len(buf) >= 32 {
			return "", nil, ErrTarget
		}
		buf = append(buf, b)
	}
	target := strings.TrimSpace(string(buf))
	if !AllowedTarget(target) {
		return "", nil, fmt.Errorf("%w: %s", ErrTarget, target)
	}
	return target, prefixConn{Conn: conn, pre: br}, nil
}

// Proxy copies both ways until either side ends.
func Proxy(left, right net.Conn) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(left, right)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(right, left)
		done <- struct{}{}
	}()
	<-done
	_ = left.SetDeadline(time.Now())
	_ = right.SetDeadline(time.Now())
	<-done
}
