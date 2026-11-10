package desktopd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// The bot CDP gate routes a TCP connection by its first request's token
// (run_5ecb9d6d: a pooled connection answered with another display's tabs
// and its WebSocket upgrade failed). Each tunnel request uses its own
// connection.
func TestDesktopTunnelTransportDoesNotReuseConnections(t *testing.T) {
	var mu sync.Mutex
	conns := map[string]bool{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		conns[r.RemoteAddr] = true
		mu.Unlock()
		_, _ = io.WriteString(w, "ok")
	}))
	srv.Start()
	defer srv.Close()
	client := &http.Client{Transport: desktopTunnelTransport}
	for i := 0; i < 3; i++ {
		resp, err := client.Get(srv.URL + "/json/list")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}
	mu.Lock()
	defer mu.Unlock()
	if len(conns) != 3 {
		t.Fatalf("3 requests used %d connection(s); the gate would pin them to one display", len(conns))
	}
}
