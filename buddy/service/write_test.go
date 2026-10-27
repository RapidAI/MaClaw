package service

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
)

// shortWriter accepts at most max bytes per Write call, like a USB CDC endpoint
// that reports a short write instead of buffering the whole frame.
type shortWriter struct {
	max int
	buf bytes.Buffer
}

func (w *shortWriter) Read([]byte) (int, error) { return 0, io.EOF }
func (w *shortWriter) Write(p []byte) (int, error) {
	if len(p) > w.max {
		p = p[:w.max]
	}
	return w.buf.Write(p)
}
func (w *shortWriter) Close() error { return nil }

func TestWriteCompletesShortWrites(t *testing.T) {
	w := &shortWriter{max: 7}
	runner := NewRunner(func() (io.ReadWriteCloser, error) { return w, nil }, testOptions())

	msg := &protocol.Message{
		Type:   protocol.TypeState,
		Proto:  protocol.Version,
		Seq:    3,
		Counts: &protocol.Counts{Total: 2, InProgress: 1, Done: 1},
		Current: []protocol.TaskItem{
			{ID: "a", Title: "整理会议纪要", Progress: -1},
		},
	}
	if err := runner.write(w, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	out := w.buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("frame missing newline: %q", out)
	}
	decoded, err := protocol.DecodeLine([]byte(strings.TrimSuffix(out, "\n")))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Type != protocol.TypeState || decoded.Seq != 3 || decoded.Counts == nil || decoded.Counts.Total != 2 {
		t.Fatalf("unexpected frame: %+v", decoded)
	}
}
