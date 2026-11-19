package edgelink

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
)

func TestBlocksAdminPathEncodings(t *testing.T) {
	blocked := []string{
		"GET /admin HTTP/1.1",
		"GET /admin/keys?x=1 HTTP/1.1",
		"GET /%61dmin HTTP/1.1",
		"GET /%61dmin/keys HTTP/1.1",
		"GET //admin HTTP/1.1",
		"GET /foo/%2e%2e/admin HTTP/1.1",
		"GET /admin%2Fkeys HTTP/1.1",
	}
	for _, line := range blocked {
		if !blocksAdminPath(line) {
			t.Fatalf("forwarded %s", line)
		}
	}
	allowed := []string{
		"GET /v1/health HTTP/1.1",
		"GET /v1/desktops/session HTTP/1.1",
		"GET /administrator HTTP/1.1",
	}
	for _, line := range allowed {
		if blocksAdminPath(line) {
			t.Fatalf("blocked %s", line)
		}
	}
}

func TestTunnelDoesNotForwardAdmin(t *testing.T) {
	hit := make(chan string, 4)
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		for {
			conn, err := upstream.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				line, _ := bufio.NewReader(conn).ReadString('\n')
				hit <- strings.TrimSpace(line)
				_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
			}(conn)
		}
	}()

	node, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	go func() {
		conn, err := node.Accept()
		if err != nil {
			return
		}
		_ = Serve(conn, map[string]string{TargetDesktopd: upstream.Addr().String()})
	}()

	client, err := net.Dial("tcp", node.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sess, err := yamux.Client(client, YamuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	admin, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteTarget(admin, TargetDesktopd); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(admin, "GET /admin/keys?x=1 HTTP/1.1\r\nHost: desktopd\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = admin.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 256)
	n, err := admin.Read(buf)
	if err != nil && n == 0 {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf[:n]), "403") {
		t.Fatalf("admin response = %q", buf[:n])
	}
	select {
	case line := <-hit:
		t.Fatalf("admin was forwarded: %s", line)
	default:
	}

	health, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteTarget(health, TargetDesktopd); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(health, "GET /v1/health HTTP/1.1\r\nHost: desktopd\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case line := <-hit:
		if !strings.Contains(line, "GET /v1/health") {
			t.Fatalf("forwarded %s", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("health was not forwarded")
	}
}
