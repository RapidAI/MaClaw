package tts

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreparePlayableVoiceMP3PassesThroughExistingMP3(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "already.mp3", data: testID3MP3()},
		{name: "voice.wav", data: testID3MP3()},
		{name: "voice.wav", data: []byte{0xff, 0xfb, 0x90, 0x64}},
	} {
		got, err := PreparePlayableVoiceMP3(context.Background(), tc.name, tc.data)
		if err != nil {
			t.Fatalf("PreparePlayableVoiceMP3(%q): %v", tc.name, err)
		}
		if string(got.Data) != string(tc.data) || got.Name != "voice.mp3" || got.MIME != "audio/mpeg" || got.Converted {
			t.Fatalf("pass-through = %+v", got)
		}
	}
}

func TestHasMP3FrameHeaderRejectsAACAndInvalidHeaders(t *testing.T) {
	if !HasMP3FrameHeader([]byte{0xff, 0xfb, 0x90, 0x64}) {
		t.Fatal("valid mp3 frame header was rejected")
	}
	if HasMP3FrameHeader([]byte{0xff, 0xf1, 0x50, 0x80}) {
		t.Fatal("adts aac header must not be treated as mp3")
	}
	if HasMP3FrameHeader([]byte{0xff, 0xe0, 0x00, 0x00}) {
		t.Fatal("invalid mp3 frame header was accepted")
	}
	if HasMP3FrameHeader([]byte{0xff, 0xfd, 0x90, 0x64}) {
		t.Fatal("mpeg layer ii frame header must not be treated as mp3")
	}
}

func TestPreparePlayableVoiceMP3ConvertsWAVToMP3(t *testing.T) {
	got, err := PreparePlayableVoiceMP3(context.Background(), "voice.wav", testWAVS16Mono(16000, []int16{0, 1000, -1000, 500, -500, 0}))
	if err != nil {
		t.Fatalf("PreparePlayableVoiceMP3(wav): %v", err)
	}
	if len(got.Data) == 0 || got.Name != "voice.mp3" || got.MIME != "audio/mpeg" || !got.Converted {
		t.Fatalf("converted playable voice = %+v", got)
	}
	if !bytes.HasPrefix(got.Data, []byte("ID3")) && !HasMP3FrameHeader(got.Data) {
		t.Fatalf("converted mp3 header = % x", got.Data[:min(4, len(got.Data))])
	}
}

func TestPreparePlayableVoiceMP3RejectsUnsupportedSource(t *testing.T) {
	_, err := PreparePlayableVoiceMP3(context.Background(), "voice.ogg", []byte("OggS"))
	if err == nil || !strings.Contains(err.Error(), "unsupported playable fallback source") {
		t.Fatalf("unsupported source err = %v", err)
	}
	_, err = PreparePlayableVoiceMP3(context.Background(), "voice.wav", []byte{0xff, 0xf1, 0x50, 0x80})
	if err == nil || !strings.Contains(err.Error(), "invalid wav data") {
		t.Fatalf("adts source err = %v", err)
	}
	_, err = PreparePlayableVoiceMP3(context.Background(), "voice.mp3", []byte("not-even-id3"))
	if err == nil || !strings.Contains(err.Error(), "unsupported playable fallback source") {
		t.Fatalf("fake mp3 source err = %v", err)
	}
	_, err = PreparePlayableVoiceMP3(context.Background(), "voice.mp3", []byte("ID3-not-a-real-mp3"))
	if err == nil || !strings.Contains(err.Error(), "unsupported playable fallback source") {
		t.Fatalf("fake id3 mp3 source err = %v", err)
	}
}

func TestPreparePlayableVoiceMP3RejectsEmptyData(t *testing.T) {
	_, err := PreparePlayableVoiceMP3(context.Background(), "voice.wav", nil)
	if err == nil || !strings.Contains(err.Error(), "empty voice data") {
		t.Fatalf("empty data err = %v", err)
	}
}

func TestEncodeWAVToMP3ProducesMP3Data(t *testing.T) {
	mp3Data, err := EncodeWAVToMP3Context(context.Background(), testWAVS16Mono(16000, []int16{0, 1000, -1000, 500, -500, 0}))
	if err != nil {
		t.Fatalf("EncodeWAVToMP3Context: %v", err)
	}
	if len(mp3Data) == 0 {
		t.Fatal("mp3 data empty")
	}
	if !bytes.HasPrefix(mp3Data, []byte("ID3")) && !HasMP3FrameHeader(mp3Data) {
		t.Fatalf("mp3 header = % x", mp3Data[:min(4, len(mp3Data))])
	}
}

func TestEncodeWAVFileToMP3Archive(t *testing.T) {
	dir := t.TempDir()
	wavPath := filepath.Join(dir, "meeting.wav")
	mp3Path := filepath.Join(dir, "meeting.mp3")
	wavData := testWAVS16Mono(16000, []int16{0, 1000, -1000, 500, -500, 0, 200, -200})
	if err := os.WriteFile(wavPath, wavData, 0o644); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	if err := EncodeWAVFileToMP3Archive(context.Background(), wavPath, mp3Path); err != nil {
		t.Fatalf("EncodeWAVFileToMP3Archive: %v", err)
	}
	got, err := os.ReadFile(mp3Path)
	if err != nil {
		t.Fatalf("read mp3: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("empty mp3 product")
	}
	if !bytes.HasPrefix(got, []byte("ID3")) && !HasMP3FrameHeader(got) {
		t.Fatalf("mp3 header = % x", got[:min(4, len(got))])
	}
}

func TestEncodeWAVFileToMP3ArchiveRejectsMissingAndTiny(t *testing.T) {
	dir := t.TempDir()
	mp3Path := filepath.Join(dir, "out.mp3")
	if err := EncodeWAVFileToMP3Archive(context.Background(), filepath.Join(dir, "missing.wav"), mp3Path); err == nil {
		t.Fatal("expected missing wav error")
	}
	tiny := filepath.Join(dir, "tiny.wav")
	if err := os.WriteFile(tiny, []byte("RIFF"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EncodeWAVFileToMP3Archive(context.Background(), tiny, mp3Path); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Fatalf("tiny wav err = %v", err)
	}
}

func TestEncodeWAVToMP3ContextRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := EncodeWAVToMP3Context(ctx, testWAVS16Mono(16000, []int16{0, 1, -1, 0}))
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("cancelled ctx err = %v", err)
	}
}

func TestEncodeWAVToMP3RejectsUnsupportedSampleRate(t *testing.T) {
	for _, sampleRate := range []int{8000, 11025, 12000, 12345} {
		_, err := EncodeWAVToMP3Context(context.Background(), testWAVS16Mono(sampleRate, []int16{0, 1, -1, 0}))
		if err == nil || !strings.Contains(err.Error(), "unsupported sample rate") {
			t.Fatalf("unsupported sample rate %d err = %v", sampleRate, err)
		}
	}
}

func TestEncodeWAVToMP3RejectsOversizedWAVBeforePCMDecode(t *testing.T) {
	dataSize := uint32((16000 * 2 * maxWAVToMP3EncodeSeconds) + 2)
	_, err := EncodeWAVToMP3Context(context.Background(), testWAVHeaderWithDataSize(1, 16000, 16, dataSize))
	if err == nil || !strings.Contains(err.Error(), "wav pcm too large") {
		t.Fatalf("oversized wav err = %v", err)
	}
}

func TestEncodeWAVToMP3RejectsMalformedPCMFrameSize(t *testing.T) {
	_, err := EncodeWAVToMP3Context(context.Background(), testWAVPCM(2, 16000, 16, []byte{0, 0}))
	if err == nil || !strings.Contains(err.Error(), "malformed wav pcm size") {
		t.Fatalf("malformed pcm err = %v", err)
	}
}

func TestEncodeWAVToMP3RejectsUnsupportedBitDepth(t *testing.T) {
	_, err := EncodeWAVToMP3Context(context.Background(), testWAVPCM(1, 16000, 12, []byte{0, 0}))
	if err == nil || !strings.Contains(err.Error(), "unsupported wav bit depth") {
		t.Fatalf("unsupported bit depth err = %v", err)
	}
}

func testWAVS16Mono(sampleRate int, samples []int16) []byte {
	dataSize := len(samples) * 2
	pcm := make([]byte, dataSize)
	for i, sample := range samples {
		putU16LE(pcm[i*2:i*2+2], uint16(sample))
	}
	return testWAVPCM(1, sampleRate, 16, pcm)
}

func testWAVPCM(channels, sampleRate, bitDepth int, pcm []byte) []byte {
	dataSize := len(pcm)
	fileSize := 36 + dataSize
	bytesPerSample := (bitDepth + 7) / 8
	blockAlign := channels * bytesPerSample
	byteRate := sampleRate * blockAlign
	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], []byte("RIFF"))
	putU32LE(buf[4:8], uint32(fileSize))
	copy(buf[8:12], []byte("WAVE"))
	copy(buf[12:16], []byte("fmt "))
	putU32LE(buf[16:20], 16)
	putU16LE(buf[20:22], 1)
	putU16LE(buf[22:24], uint16(channels))
	putU32LE(buf[24:28], uint32(sampleRate))
	putU32LE(buf[28:32], uint32(byteRate))
	putU16LE(buf[32:34], uint16(blockAlign))
	putU16LE(buf[34:36], uint16(bitDepth))
	copy(buf[36:40], []byte("data"))
	putU32LE(buf[40:44], uint32(dataSize))
	copy(buf[44:], pcm)
	return buf
}

func testWAVHeaderWithDataSize(channels, sampleRate, bitDepth int, dataSize uint32) []byte {
	bytesPerSample := (bitDepth + 7) / 8
	blockAlign := channels * bytesPerSample
	byteRate := sampleRate * blockAlign
	buf := make([]byte, 44)
	copy(buf[0:4], []byte("RIFF"))
	putU32LE(buf[4:8], 36+dataSize)
	copy(buf[8:12], []byte("WAVE"))
	copy(buf[12:16], []byte("fmt "))
	putU32LE(buf[16:20], 16)
	putU16LE(buf[20:22], 1)
	putU16LE(buf[22:24], uint16(channels))
	putU32LE(buf[24:28], uint32(sampleRate))
	putU32LE(buf[28:32], uint32(byteRate))
	putU16LE(buf[32:34], uint16(blockAlign))
	putU16LE(buf[34:36], uint16(bitDepth))
	copy(buf[36:40], []byte("data"))
	putU32LE(buf[40:44], dataSize)
	return buf
}

func putU16LE(dst []byte, v uint16) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
}

func putU32LE(dst []byte, v uint32) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func testID3MP3() []byte {
	return append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}, []byte{0xff, 0xfb, 0x90, 0x64}...)
}
