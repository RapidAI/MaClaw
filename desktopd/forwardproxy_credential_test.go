package desktopd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestForwardProxyDoesNotForwardRotatedProxyBearerToTarget(t *testing.T) {
	keys := NewProxyKeySource(t.TempDir(), "fallback")
	if err := keys.Set("rotated-secret"); err != nil {
		t.Fatal(err)
	}
	proxy := NewForwardProxy(ProxyConfig{Addr: "127.0.0.1:0", Token: "fallback", Keys: keys})
	proxy.dialHook = func(ctx context.Context, address string) (net.Conn, error) {
		dialer := &net.Dialer{}
		return dialer.DialContext(ctx, "tcp", address)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		_ = proxy.Stop()
	})
	if err := proxy.Start(ctx); err != nil {
		t.Fatal(err)
	}
	backend := echoBackend(t)
	conn, err := net.Dial("tcp", proxy.listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, _ = fmt.Fprintf(conn, "GET http://%s/auth HTTP/1.1\r\nAuthorization: Bearer rotated-secret\r\nHost: %s\r\nConnection: close\r\n\r\n", backend, backend)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %q", resp.StatusCode, body)
	}
	if got := string(body); got != "auth:" {
		t.Fatalf("target received the proxy credential or lost the header unexpectedly: %q", got)
	}
	if strings.Contains(string(body), "rotated-secret") {
		t.Fatal("rotated proxy token reached the target site")
	}
}
