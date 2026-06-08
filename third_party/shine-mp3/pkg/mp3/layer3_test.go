package mp3

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteMatchesManualChunkingMono(t *testing.T) {
	testWriteMatchesManualChunking(t, 16000, 1)
}

func TestWriteMatchesManualChunkingStereo(t *testing.T) {
	testWriteMatchesManualChunking(t, 44100, 2)
}

func TestNewEncoderRejectsUnsupportedConfig(t *testing.T) {
	for _, tc := range []struct {
		sampleRate int
		channels   int
	}{
		{sampleRate: 0, channels: 1},
		{sampleRate: 12345, channels: 1},
		{sampleRate: 16000, channels: 0},
		{sampleRate: 16000, channels: 3},
	} {
		if enc := NewEncoder(tc.sampleRate, tc.channels); enc != nil {
			t.Fatalf("NewEncoder(%d, %d) = %#v, want nil", tc.sampleRate, tc.channels, enc)
		}
	}
}

func testWriteMatchesManualChunking(t *testing.T, sampleRate, channels int) {
	t.Helper()

	gotEnc := NewEncoder(sampleRate, channels)
	wantEnc := NewEncoder(sampleRate, channels)
	data := testPCMInput(int(gotEnc.samplesPerPass())*channels*3 + channels*17)

	var got bytes.Buffer
	if err := gotEnc.Write(&got, data); err != nil {
		t.Fatalf("Write: %v", err)
	}

	want, err := encodeManualChunking(wantEnc, data)
	if err != nil {
		t.Fatalf("encodeManualChunking: %v", err)
	}

	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("Write output mismatch: got=%d want=%d", got.Len(), len(want))
	}
}

func encodeManualChunking(enc *Encoder, data []int16) ([]byte, error) {
	samplesPerPass := int(enc.samplesPerPass())
	stride := int(enc.Wave.Channels)
	if stride < 1 {
		stride = 1
	}
	chunkSize := samplesPerPass * stride

	var out bytes.Buffer
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]
		if len(chunk) == 0 {
			continue
		}
		if len(chunk) < chunkSize {
			padded := make([]int16, chunkSize)
			copy(padded, chunk)
			chunk = padded
		}
		frame, written := enc.encodeBufferInterleaved(&chunk[0])
		if err := binary.Write(&out, binary.LittleEndian, frame[:written]); err != nil {
			return nil, err
		}
	}
	return out.Bytes(), nil
}

func testPCMInput(n int) []int16 {
	out := make([]int16, n)
	for i := range out {
		out[i] = int16((i*73)%65535 - 32768)
	}
	return out
}
