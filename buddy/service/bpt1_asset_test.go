package service

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/buddy/protocol"
)

func TestRunnerSendsPetAssetBPT1(t *testing.T) {
	runner, host := startRunnerForTest(t)
	frame := make([]byte, protocol.AssetFrameBytes)
	for i := range frame {
		frame[i] = byte(i % 251)
	}
	runner.SetPetAsset([][]byte{frame}, 450)

	reader := bufio.NewReader(host)
	helloLine, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read host hello: %v", err)
	}
	hello, err := protocol.DecodeLine(helloLine)
	if err != nil || hello.Type != protocol.TypeHello {
		t.Fatalf("first frame = %v (%v), want hello", hello, err)
	}
	writeTestFrame(t, host, &protocol.Message{
		Type:      protocol.TypeHello,
		Proto:     protocol.Version,
		FW:        "test",
		Board:     "test",
		AssetMode: protocol.AssetModeBPT1,
	})

	got := map[int][]byte{}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && len(got) < (protocol.AssetFrameBytes+protocol.BPT1MaxData-1)/protocol.BPT1MaxData {
		peek, err := reader.Peek(1)
		if err != nil {
			t.Fatalf("peek: %v", err)
		}
		if peek[0] == '{' {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				t.Fatalf("json line: %v", err)
			}
			if _, err := protocol.DecodeLine(line); err != nil {
				continue
			}
			continue
		}
		prefix := make([]byte, 28)
		if _, err := io.ReadFull(reader, prefix); err != nil {
			t.Fatalf("bpt1 prefix: %v", err)
		}
		dataLen := int(binary.LittleEndian.Uint16(prefix[24:26]))
		rest := make([]byte, dataLen)
		if _, err := io.ReadFull(reader, rest); err != nil {
			t.Fatalf("bpt1 data: %v", err)
		}
		chunk, n, err := protocol.DecodeBPT1(append(prefix, rest...))
		if err != nil || n != 28+dataLen {
			t.Fatalf("decode bpt1: %v n=%d", err, n)
		}
		got[chunk.Offset] = chunk.Data
		last := chunk.Offset+len(chunk.Data) == chunk.Total
		writeTestFrame(t, host, &protocol.Message{
			Type:  protocol.TypeAssetAck,
			Proto: protocol.Version,
			AssetAck: &protocol.AssetAck{
				Frame: chunk.Frame,
				Bytes: chunk.Offset + len(chunk.Data),
				Ready: last,
			},
		})
		if last {
			break
		}
	}
	assembled := make([]byte, 0, len(frame))
	for off := 0; off < len(frame); off += protocol.BPT1MaxData {
		part, ok := got[off]
		if !ok {
			t.Fatalf("missing bpt1 chunk at %d (have %d)", off, len(got))
		}
		assembled = append(assembled, part...)
	}
	if !bytes.Equal(assembled, frame) {
		t.Fatal("reassembled bpt1 asset differs")
	}
}

func TestStateAckReportsLatency(t *testing.T) {
	runner, host := startRunnerForTest(t)
	runner.SetSnapshot(protocol.Snapshot{
		Counts:    protocol.Counts{Total: 1, Pending: 1},
		UpdatedAt: time.Now().UnixMilli(),
	})
	reader := protocol.NewLineReader(host, protocol.MaxLineBytes)
	line, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	if hello, err := protocol.DecodeLine(line); err != nil || hello.Type != protocol.TypeHello {
		t.Fatalf("hello: %v %v", hello, err)
	}
	writeTestFrame(t, host, &protocol.Message{Type: protocol.TypeHello, Proto: protocol.Version, FW: "test", Board: "test"})

	var state *protocol.Message
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && state == nil {
		line, err := reader.Next()
		if err != nil {
			t.Fatal(err)
		}
		msg, err := protocol.DecodeLine(line)
		if err != nil {
			continue
		}
		if msg.Type == protocol.TypeState {
			state = msg
		}
	}
	if state == nil || state.TsMS == 0 {
		t.Fatalf("state missing ts_ms: %+v", state)
	}
	writeTestFrame(t, host, &protocol.Message{
		Type:  protocol.TypeStateAck,
		Proto: protocol.Version,
		Page:  "tasks",
		TsMS:  state.TsMS - 40,
	})
	deadline = time.Now().Add(time.Second)
	var got int64
	for time.Now().Before(deadline) {
		got = runner.Status().StateLatencyMS
		if got >= 40 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got < 40 || got > 5000 {
		t.Fatalf("state latency %d", got)
	}
}
