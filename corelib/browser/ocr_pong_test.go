package browser

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"
)

func TestReadOCRPongSkipsWarningLines(t *testing.T) {
	// A broken-environment sidecar prints warning lines (e.g. OpenCV's numpy
	// notice) before the JSON pong; readiness must wait for the actual pong.
	in := "OpenCV bindings requires \"numpy\" package.\n" +
		"some other startup noise\n" +
		`{"status":"ok"}` + "\n"
	skipped, err := readOCRPong(bufio.NewScanner(strings.NewReader(in)), 5*time.Second)
	if err != nil {
		t.Fatalf("want pong, got err=%v", err)
	}
	if len(skipped) != 2 || skipped[0] != `OpenCV bindings requires "numpy" package.` {
		t.Fatalf("skipped = %#v", skipped)
	}
}

func TestReadOCRPongEOFIsError(t *testing.T) {
	// Process died during import (e.g. numpy ABI mismatch): stdout reaches
	// EOF after a warning line. Must fail, not report ready.
	in := "OpenCV bindings requires \"numpy\" package.\n"
	_, err := readOCRPong(bufio.NewScanner(strings.NewReader(in)), 5*time.Second)
	if err == nil {
		t.Fatal("EOF before pong must be an error")
	}
	if !strings.Contains(err.Error(), "exited") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadOCRPongIgnoresWrongJSON(t *testing.T) {
	// JSON lines that are not a pong (status != ok) are skipped like noise.
	in := `{"error":"something"}` + "\n" + `{"status":"ok"}` + "\n"
	skipped, err := readOCRPong(bufio.NewScanner(strings.NewReader(in)), 5*time.Second)
	if err != nil {
		t.Fatalf("want pong, got err=%v", err)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped = %#v", skipped)
	}
}

func TestReadOCRPongTimeout(t *testing.T) {
	// A hung sidecar (no output at all) must fail after the timeout.
	r, _ := io.Pipe()
	defer r.Close()
	_, err := readOCRPong(bufio.NewScanner(r), 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("want timeout error, got %v", err)
	}
}
