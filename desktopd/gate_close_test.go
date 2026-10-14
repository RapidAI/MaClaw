package desktopd

import (
	"strings"
	"testing"
)

// A viewer that disconnects must reach the backend as EOF, or websockify
// keeps one child per closed viewer forever.
func TestGatePipePropagatesViewerClose(t *testing.T) {
	out := runSupervisorPython(t, `
import socket, threading, time
back = socket.socket(); back.bind(("127.0.0.1", 0)); back.listen(1)
bport = back.getsockname()[1]
probe = socket.socket(); probe.bind(("127.0.0.1", 0)); gport = probe.getsockname()[1]; probe.close()
threading.Thread(target=sup.serve_gate, args=(gport, bport, "tok"), daemon=True).start()
time.sleep(0.3)
c = socket.create_connection(("127.0.0.1", gport))
c.sendall(b"GET / HTTP/1.1\r\nAuthorization: Bearer tok\r\n\r\n")
b, _ = back.accept()
b.settimeout(3)
b.recv(4096)
c.close()
try:
    print("EOF" if b.recv(10) == b"" else "DATA")
except socket.timeout:
    print("HANG")
`)
	if !strings.Contains(out, "EOF") {
		t.Fatalf("backend did not see viewer close: %s", out)
	}
}
