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

// A client that never finishes its request head must not stall the gate for
// everyone else: the second, authorized client is relayed at once.
func TestGateSlowClientDoesNotBlockOthers(t *testing.T) {
	out := runSupervisorPython(t, `
import socket, threading, time
back = socket.socket(); back.bind(("127.0.0.1", 0)); back.listen(4)
bport = back.getsockname()[1]
probe = socket.socket(); probe.bind(("127.0.0.1", 0)); gport = probe.getsockname()[1]; probe.close()
threading.Thread(target=sup.serve_gate, args=(gport, bport, "tok"), daemon=True).start()
time.sleep(0.3)
slow = socket.create_connection(("127.0.0.1", gport))
slow.sendall(b"GET / HTTP/1.1\r\n")
time.sleep(0.2)
start = time.time()
c = socket.create_connection(("127.0.0.1", gport))
c.sendall(b"GET / HTTP/1.1\r\nAuthorization: Bearer tok\r\n\r\n")
back.settimeout(3)
try:
    b, _ = back.accept()
    print("RELAYED %.1f" % (time.time() - start))
except socket.timeout:
    print("BLOCKED")
`)
	if !strings.Contains(out, "RELAYED") {
		t.Fatalf("slow client blocked the gate: %s", out)
	}
}
