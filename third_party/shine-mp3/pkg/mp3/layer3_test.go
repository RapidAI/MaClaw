package mp3

import (
	"bytes"
	"encoding/binary"
	"fmt"
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

// TestWriteProducesContiguousFrameStream guards the desync regression class:
// every encoded frame must start exactly where the previous frame's declared
// size ends. A frame budget that overflows the 12-bit part2_3_length field
// (e.g. 128 kbps MPEG-II mono at 16 kHz) used to make every frame ~409 bits
// short, so decoders walked off the stream after the first frame.
func TestWriteProducesContiguousFrameStream(t *testing.T) {
	for _, sampleRate := range []int{8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000} {
		for _, channels := range []int{1, 2} {
			t.Run(fmt.Sprintf("%dHz/%dch", sampleRate, channels), func(t *testing.T) {
				enc := NewEncoder(sampleRate, channels)
				if enc == nil {
					t.Fatalf("NewEncoder(%d, %d) = nil", sampleRate, channels)
				}
				data := testPCMInput(sampleRate/2 * channels) // ~0.5 s
				var out bytes.Buffer
				if err := enc.Write(&out, data); err != nil {
					t.Fatalf("Write: %v", err)
				}
				if err := enc.Flush(&out); err != nil {
					t.Fatalf("Flush: %v", err)
				}
				walkMP3FrameStream(t, out.Bytes(), sampleRate, int(enc.Mpeg.Bitrate))
			})
		}
	}
}

// TestFlushContract locks in Flush behavior: safe on a fresh encoder, drains
// only whole pending bytes after Write, and is idempotent afterwards.
func TestFlushContract(t *testing.T) {
	enc := NewEncoder(44100, 2)
	var out bytes.Buffer
	if err := enc.Flush(&out); err != nil {
		t.Fatalf("Flush on fresh encoder: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("Flush on fresh encoder wrote %d bytes", out.Len())
	}
	if err := enc.Write(&out, testPCMInput(44100)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	before := out.Len()
	if err := enc.Flush(&out); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if flushed := out.Len() - before; flushed < 0 || flushed > 3 {
		t.Fatalf("Flush wrote %d bytes, want 0..3", flushed)
	}
	if err := enc.Flush(&out); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	if got := out.Len() - before; got > 3 {
		t.Fatalf("Flush is not idempotent: %d bytes total", got)
	}
}

// TestNewEncoderSelectsRepresentableBitrate locks in the auto-selected bitrate
// for the configurations that motivated it.
func TestNewEncoderSelectsRepresentableBitrate(t *testing.T) {
	for _, tc := range []struct {
		sampleRate, channels, wantBitrate int
	}{
		{16000, 1, 112},  // 128k would need 4504 bits/granule (> 4095)
		{16000, 2, 128},  // stereo splits the budget across channels
		{44100, 2, 128},  // MPEG-I: two granules per frame
		{8000, 1, 56},    // MPEG-2.5 mono: 64k already overflows
		{11025, 1, 64},   // 80k overflows: (835*8-104)/1 = 6576 > 4095... 64k fits
	} {
		enc := NewEncoder(tc.sampleRate, tc.channels)
		if enc == nil {
			t.Fatalf("NewEncoder(%d, %d) = nil", tc.sampleRate, tc.channels)
		}
		if got := int(enc.Mpeg.Bitrate); got != tc.wantBitrate {
			t.Fatalf("NewEncoder(%d, %d) bitrate = %d, want %d", tc.sampleRate, tc.channels, got, tc.wantBitrate)
		}
	}
}

// walkMP3FrameStream verifies that mp3 is exactly tiled by valid MPEG audio
// frames with the expected sample rate and bitrate: zero desyncs, at least a
// few frames, and nothing left over after the final frame.
func walkMP3FrameStream(t *testing.T, mp3 []byte, wantSampleRate, wantBitrate int) {
	t.Helper()

	mpeg1Bitrates := [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	lsfBitrates := [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	sampleRatesByVersion := [4][3]int{
		{11025, 12000, 8000}, // MPEG-2.5
		{},                   // reserved
		{22050, 24000, 16000}, // MPEG-II
		{44100, 48000, 32000}, // MPEG-I
	}

	off := 0
	frames := 0
	for off+4 <= len(mp3) {
		b0, b1, b2 := mp3[off], mp3[off+1], mp3[off+2]
		if b0 != 0xFF || b1&0xE0 != 0xE0 {
			t.Fatalf("desync at offset %d after %d frames: %02x %02x %02x", off, frames, b0, b1, b2)
		}
		version := int(b1>>3) & 3
		layer := int(b1>>1) & 3
		bitrateIdx := int(b2>>4) & 0xF
		sampleRateIdx := int(b2>>2) & 3
		padding := int(b2>>1) & 1
		if version == 1 || layer != 1 || bitrateIdx == 0 || bitrateIdx == 15 || sampleRateIdx == 3 {
			t.Fatalf("frame %d at %d: invalid header %02x %02x %02x", frames, off, b0, b1, b2)
		}
		sampleRate := sampleRatesByVersion[version][sampleRateIdx]
		bitrate := mpeg1Bitrates[bitrateIdx]
		frameSize := 144 * bitrate * 1000 / sampleRate
		if version != 3 {
			bitrate = lsfBitrates[bitrateIdx]
			frameSize = 72 * bitrate * 1000 / sampleRate
		}
		frameSize += padding
		if sampleRate != wantSampleRate || bitrate != wantBitrate {
			t.Fatalf("frame %d: got %d Hz/%d kbps, want %d Hz/%d kbps", frames, sampleRate, bitrate, wantSampleRate, wantBitrate)
		}
		if off+frameSize > len(mp3) {
			t.Fatalf("frame %d at %d truncated: needs %d bytes, %d left", frames, off, frameSize, len(mp3)-off)
		}
		off += frameSize
		frames++
	}
	if frames < 3 {
		t.Fatalf("only %d frames in %d bytes", frames, len(mp3))
	}
	// Flush ends the stream exactly on the last frame's boundary.
	if tail := len(mp3) - off; tail != 0 {
		t.Fatalf("stream tail = %d bytes after %d frames (want 0)", tail, frames)
	}
}
