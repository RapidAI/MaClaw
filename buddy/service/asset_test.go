package service

import (
	"bytes"
	"encoding/base64"
	"io"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
)

// The device acks every committed chunk; the runner must not write the next
// one before that ack. The test doubles as a fake device: it answers the host
// hello, then acks each asset chunk as it is parsed.
func TestRunnerSendsPetAsset(t *testing.T) {
	runner, host := startRunnerForTest(t)

	frame := make([]byte, protocol.AssetFrameBytes)
	for i := range frame {
		frame[i] = byte(i % 251)
	}
	runner.SetPetAsset([][]byte{frame}, 450)

	reader := protocol.NewLineReader(host, protocol.MaxLineBytes)
	line, err := reader.Next()
	if err != nil {
		t.Fatalf("read host hello: %v", err)
	}
	hello, err := protocol.DecodeLine(line)
	if err != nil || hello.Type != protocol.TypeHello {
		t.Fatalf("first frame = %v (%v), want hello", hello, err)
	}
	writeTestFrame(t, host, &protocol.Message{
		Type:  protocol.TypeHello,
		Proto: protocol.Version,
		FW:    "test",
		Board: "test",
	})

	chunks := map[int][]byte{}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.Next()
		if err != nil {
			t.Fatalf("read asset frame: %v", err)
		}
		m, err := protocol.DecodeLine(line)
		if err != nil {
			continue // e.g. the clock that precedes the asset
		}
		if m.Type != protocol.TypeAssetChunk || m.Asset == nil {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(m.Asset.Data)
		if err != nil {
			t.Fatalf("decode chunk at %d: %v", m.Asset.Offset, err)
		}
		chunks[m.Asset.Offset] = data
		last := m.Asset.Offset+len(data) == m.Asset.Total &&
			m.Asset.Frame == m.Asset.Frames-1
		writeTestFrame(t, host, &protocol.Message{
			Type:  protocol.TypeAssetAck,
			Proto: protocol.Version,
			AssetAck: &protocol.AssetAck{
				Frame: m.Asset.Frame,
				Bytes: m.Asset.Offset + len(data),
				Ready: last,
			},
		})
		if last {
			break
		}
	}
	if len(chunks) != (protocol.AssetFrameBytes+511)/512 {
		t.Fatalf("got %d chunks, want %d", len(chunks),
			(protocol.AssetFrameBytes+511)/512)
	}
	assembled := make([]byte, 0, protocol.AssetFrameBytes)
	for off := 0; off < protocol.AssetFrameBytes; off += 512 {
		part, ok := chunks[off]
		if !ok {
			t.Fatalf("missing chunk at offset %d", off)
		}
		assembled = append(assembled, part...)
	}
	if !bytes.Equal(assembled, frame) {
		t.Fatal("reassembled asset differs from the source frame")
	}
	// The session goroutine consumes the final ack just after the write
	// returns, so allow it a moment before inspecting the status.
	readyDeadline := time.Now().Add(time.Second)
	for time.Now().Before(readyDeadline) && !runner.Status().PetReady {
		time.Sleep(5 * time.Millisecond)
	}
	if !runner.Status().PetReady {
		t.Fatal("status does not report the pet as ready")
	}

	// SetPetAsset raced with the connect-time send in this test; the wake it
	// queued must not trigger a second full transfer.
	_ = host.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		line, err := reader.Next()
		if err != nil {
			break
		}
		m, err := protocol.DecodeLine(line)
		if err == nil && m.Type == protocol.TypeAssetChunk {
			t.Fatal("pet asset was sent twice")
		}
	}
}

func writeTestFrame(t *testing.T, w io.Writer, m *protocol.Message) {
	t.Helper()
	frame, err := protocol.Encode(m)
	if err != nil {
		t.Fatalf("encode %s: %v", m.Type, err)
	}
	if _, err := w.Write(frame); err != nil {
		t.Fatalf("write %s: %v", m.Type, err)
	}
}
