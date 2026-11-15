package edgelink

import (
	"bufio"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/hashicorp/yamux"
)

// YamuxConfig matches the Hub side (localrelay.YamuxConfig): 20s keepalives.
func YamuxConfig() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.KeepAliveInterval = 20 * time.Second
	cfg.ConnectionWriteTimeout = 30 * time.Second
	cfg.StreamOpenTimeout = 30 * time.Second
	cfg.MaxStreamWindowSize = 1 << 20
	cfg.LogOutput = nil
	cfg.Logger = log.New(io.Discard, "", 0)
	return cfg
}

// Serve accepts Hub-opened streams and forwards each one to routes[target].
// A target that is not in routes is closed. The function returns when the
// session ends.
func Serve(conn net.Conn, routes map[string]string) error {
	copied := make(map[string]string, len(routes))
	for target, addr := range routes {
		copied[target] = addr
	}
	sess, err := yamux.Server(conn, YamuxConfig())
	if err != nil {
		return err
	}
	defer sess.Close()
	// Bound concurrent dials. A burst of streams would otherwise open one
	// goroutine and one TCP connection each, with no backpressure.
	slots := make(chan struct{}, 32)
	for {
		stream, err := sess.AcceptStream()
		if err != nil {
			return err
		}
		slots <- struct{}{}
		go func(stream net.Conn) {
			defer func() { <-slots }()
			serveOne(stream, copied)
		}(stream)
	}
}

func serveOne(stream net.Conn, routes map[string]string) {
	defer stream.Close()
	target, conn, err := ReadTargetConn(stream)
	if err != nil {
		return
	}
	addr := routes[target]
	if addr == "" {
		return
	}
	if target == TargetDesktopd {
		blocked, next, err := refuseDesktopAdmin(conn)
		if err != nil || blocked {
			return
		}
		conn = next
	}
	upstream, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	Proxy(conn, upstream)
}

// refuseDesktopAdmin drops a tunnel request for the operator panel.
// Other requests are returned with the peeked bytes put back.
func refuseDesktopAdmin(conn net.Conn) (bool, net.Conn, error) {
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil && line == "" {
		return false, nil, err
	}
	restored := prefixConn{Conn: conn, pre: io.MultiReader(strings.NewReader(line), br)}
	if !blocksAdminPath(line) {
		return false, restored, nil
	}
	_, _ = io.WriteString(conn, "HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	return true, nil, nil
}

func blocksAdminPath(requestLine string) bool {
	fields := strings.Fields(requestLine)
	if len(fields) < 2 {
		return false
	}
	path := fields[1]
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	return path == "/admin" || strings.HasPrefix(path, "/admin/")
}
