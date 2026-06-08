package audioconv

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/audioformat"
	shinemp3 "github.com/braheezy/shine-mp3/pkg/mp3"
)

func TestToWAVRoutesCompressedAudioThroughNativeAdapter(t *testing.T) {
	original := compressedAudioToWAV
	defer func() { compressedAudioToWAV = original }()
	compressedAudioToWAV = func(data []byte, format string) ([]byte, error) {
		if !bytes.Equal(data, []byte("ID3-data")) || format != FormatMP3 {
			t.Fatalf("compressed adapter input = %q format=%q", data, format)
		}
		return []byte("RIFF\x00\x00\x00\x00WAVE"), nil
	}
	got, err := ToWAV([]byte("ID3-data"), FormatMP3)
	if err != nil {
		t.Fatalf("ToWAV mp3: %v", err)
	}
	if string(got) != "RIFF\x00\x00\x00\x00WAVE" {
		t.Fatalf("ToWAV mp3 = %q", got)
	}
}

func TestToWAVDecodesMP3Natively(t *testing.T) {
	mp3Data := testMP3(t)
	wav, err := ToWAV(mp3Data, FormatMP3)
	if err != nil {
		t.Fatalf("ToWAV mp3: %v", err)
	}
	if !bytes.HasPrefix(wav, []byte("RIFF")) || string(wav[8:12]) != "WAVE" {
		t.Fatalf("not wav output: %q", wav[:min(len(wav), 12)])
	}
	channels, sampleRate, bitsPerSample, err := parseFmtChunk(wav)
	if err != nil {
		t.Fatalf("parse fmt: %v", err)
	}
	if channels != TargetChannels || sampleRate != TargetSampleRate || bitsPerSample != TargetBitsPerSamp {
		t.Fatalf("wav format = %dch/%dHz/%dbit", channels, sampleRate, bitsPerSample)
	}
}

func TestToWAVRejectsInvalidMP3Natively(t *testing.T) {
	_, err := ToWAV([]byte("ID3-not-a-valid-mp3"), FormatMP3)
	if err == nil || !strings.Contains(err.Error(), "mp3 decode init failed") {
		t.Fatalf("invalid mp3 err = %v", err)
	}
}

func TestToWAVRejectsOversizedInputBeforeDecode(t *testing.T) {
	_, err := ToWAV(make([]byte, maxNativeAudioInputBytes+1), FormatWAV)
	if err == nil || !strings.Contains(err.Error(), "input audio too large") {
		t.Fatalf("oversized input err = %v", err)
	}
}

func TestExtractOggOpusPacketsJoinsPacketsAcrossPages(t *testing.T) {
	audio := append(bytes.Repeat([]byte{0x7f}, 255), bytes.Repeat([]byte{0x42}, 45)...)
	ogg := append(testOggPage(0, []byte("OpusHead")), testOggPage(1, audio[:255])...)
	ogg = append(ogg, testOggPageWithHeader(2, oggHeaderContinuation, audio[255:])...)

	packets, err := extractOggOpusPackets(ogg)
	if err != nil {
		t.Fatalf("extractOggOpusPackets: %v", err)
	}
	if len(packets) != 1 || !bytes.Equal(packets[0], audio) {
		t.Fatalf("packets = %d firstLen=%d", len(packets), len(packets[0]))
	}
}

func TestExtractOggOpusPacketsRejectsUnexpectedContinuationPage(t *testing.T) {
	ogg := append(testOggPage(0, []byte("OpusHead")), testOggPageWithHeader(1, oggHeaderContinuation, []byte("audio"))...)
	_, err := extractOggOpusPackets(ogg)
	if err == nil || !strings.Contains(err.Error(), "continuation page without pending packet") {
		t.Fatalf("unexpected continuation err = %v", err)
	}
}

func TestExtractOggOpusPacketsRejectsTruncatedPacketAtEOF(t *testing.T) {
	ogg := append(testOggPage(0, []byte("OpusHead")), testOggPage(1, bytes.Repeat([]byte{0x7f}, 255))...)
	_, err := extractOggOpusPackets(ogg)
	if err == nil || !strings.Contains(err.Error(), "truncated packet") {
		t.Fatalf("truncated ogg err = %v", err)
	}
}

func TestToWAVNormalizesPCM8StereoWAV(t *testing.T) {
	wavIn := testWAVPCM(1, 2, 8000, 8, []byte{
		128, 128,
		160, 96,
		255, 0,
		128, 128,
	})
	wav, err := ToWAV(wavIn, FormatWAV)
	if err != nil {
		t.Fatalf("ToWAV 8-bit stereo wav: %v", err)
	}
	channels, sampleRate, bitsPerSample, err := parseFmtChunk(wav)
	if err != nil {
		t.Fatalf("parse fmt: %v", err)
	}
	if channels != TargetChannels || sampleRate != TargetSampleRate || bitsPerSample != TargetBitsPerSamp {
		t.Fatalf("wav format = %dch/%dHz/%dbit", channels, sampleRate, bitsPerSample)
	}
	pcm, err := extractWAVData(wav)
	if err != nil {
		t.Fatalf("extract wav data: %v", err)
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		t.Fatalf("normalized pcm size = %d", len(pcm))
	}
}

func TestStereoToMonoAveragesInterleavedS16Frames(t *testing.T) {
	stereo := make([]byte, 8)
	writeS16LE(stereo, 0, 1000)
	writeS16LE(stereo, 1, -1000)
	writeS16LE(stereo, 2, 3000)
	writeS16LE(stereo, 3, 1000)
	mono := stereoToMono(stereo)
	if len(mono) != 4 {
		t.Fatalf("mono size = %d, want 4", len(mono))
	}
	if got := readS16LE(mono, 0); got != 0 {
		t.Fatalf("first mono sample = %d, want 0", got)
	}
	if got := readS16LE(mono, 1); got != 2000 {
		t.Fatalf("second mono sample = %d, want 2000", got)
	}
}

func TestToWAVRejectsNonPCMWAV(t *testing.T) {
	wavIn := testWAVPCM(3, 1, TargetSampleRate, 32, []byte{0, 0, 0, 0})
	_, err := ToWAV(wavIn, FormatWAV)
	if err == nil || !strings.Contains(err.Error(), "unsupported WAV format") {
		t.Fatalf("non-pcm wav err = %v", err)
	}
}

func TestToWAVRejectsTargetWAVWithoutDataChunk(t *testing.T) {
	wavIn := testWAVPCMHeaderOnly(1, TargetSampleRate, 16)
	_, err := ToWAV(wavIn, FormatWAV)
	if err == nil || !strings.Contains(err.Error(), "WAV data chunk not found") {
		t.Fatalf("missing data chunk err = %v", err)
	}
}

func TestToWAVRejectsEmptyDataChunk(t *testing.T) {
	wavIn := testWAVPCM(1, 1, TargetSampleRate, 16, nil)
	_, err := ToWAV(wavIn, FormatWAV)
	if err == nil || !strings.Contains(err.Error(), "empty WAV PCM data") {
		t.Fatalf("empty data chunk err = %v", err)
	}
}

func TestToWAVRejectsTruncatedDataChunk(t *testing.T) {
	wavIn := testWAVPCM(1, 1, TargetSampleRate, 16, []byte{0, 0})
	putU32LE(wavIn[40:44], 8)
	_, err := ToWAV(wavIn, FormatWAV)
	if err == nil || !strings.Contains(err.Error(), "malformed WAV data chunk size") {
		t.Fatalf("truncated data chunk err = %v", err)
	}
}

func TestToWAVRejectsOddSizedChunkWithoutPadding(t *testing.T) {
	wavIn := testWAVWithOddJunkMissingPadding()
	_, err := ToWAV(wavIn, FormatWAV)
	if err == nil || !strings.Contains(err.Error(), "malformed WAV JUNK chunk padding") {
		t.Fatalf("missing chunk padding err = %v", err)
	}
}

func TestToWAVRejectsZeroSampleRateWAV(t *testing.T) {
	wavIn := testWAVPCM(1, 1, 0, 16, []byte{0, 0})
	_, err := ToWAV(wavIn, FormatWAV)
	if err == nil || !strings.Contains(err.Error(), "invalid WAV sample rate") {
		t.Fatalf("zero sample rate err = %v", err)
	}
}

func TestToWAVRejectsM4AAndAACWithoutFFmpegFallback(t *testing.T) {
	for _, format := range []string{FormatM4A, FormatAAC} {
		_, err := ToWAV([]byte("compressed"), format)
		if err == nil || !strings.Contains(err.Error(), "native "+format+" decode is not supported") {
			t.Fatalf("ToWAV(%s) err = %v", format, err)
		}
	}
}

func TestDetectCompressedAudioFormats(t *testing.T) {
	if got := detectFormat(append([]byte{'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0}, []byte{0xff, 0xfb, 0x90, 0x64}...)); got != FormatMP3 {
		t.Fatalf("detect id3 mp3 = %q", got)
	}
	if got := detectFormat([]byte("ID3data")); got != "" {
		t.Fatalf("detect fake id3 mp3 = %q", got)
	}
	if got := detectFormat([]byte{0xff, 0xfb, 0x90, 0x64}); got != FormatMP3 {
		t.Fatalf("detect mp3 frame = %q", got)
	}
	if got := detectFormat([]byte{0xff, 0xfd, 0x90, 0x64}); got != "" {
		t.Fatalf("detect mpeg layer ii frame = %q", got)
	}
	if got := detectFormat([]byte{0xff, 0xf1, 0x50, 0x80}); got != FormatAAC {
		t.Fatalf("detect adts aac = %q", got)
	}
	if audioformat.LooksLikeMP3Frame([]byte{0xff, 0xf1, 0x50, 0x80}) {
		t.Fatalf("adts aac header must not be treated as mp3")
	}
	if audioformat.LooksLikeMP3Frame([]byte{0xff, 0xe0, 0x00, 0x00}) {
		t.Fatalf("invalid mp3 frame header was accepted")
	}
	if got := detectFormat([]byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00")); got != FormatM4A {
		t.Fatalf("detect m4a = %q", got)
	}
}

func TestToWAVDoesNotTreatAMRAsSilk(t *testing.T) {
	if _, err := ToWAV([]byte("#!AMR\npayload"), "amr"); err == nil {
		t.Fatalf("ToWAV amr should be unsupported until true AMR decode is implemented")
	}
}

func TestSilkToWAVStripsWeChatPrefixBeforeDecode(t *testing.T) {
	original := silkDecodeBuffToPCM
	defer func() { silkDecodeBuffToPCM = original }()
	silkDecodeBuffToPCM = func(data []byte, sampleRate int) ([]byte, error) {
		if sampleRate != silkDecodeSampleRate {
			t.Fatalf("sampleRate = %d", sampleRate)
		}
		if !bytes.Equal(data, []byte("#!SILK_V3\npayload")) {
			t.Fatalf("decoder received %q", data)
		}
		return nil, fmt.Errorf("stop after input assertion")
	}
	_, err := ToWAV([]byte("\x02#!SILK_V3\npayload"), FormatSilk)
	if err == nil || !strings.Contains(err.Error(), "stop after input assertion") {
		t.Fatalf("silk prefix regression err = %v", err)
	}
}

func TestHasCompressedAudioDecoderIsNative(t *testing.T) {
	if !HasCompressedAudioDecoder() {
		t.Fatal("native compressed audio decoder should be available")
	}
}

func testMP3(t *testing.T) []byte {
	t.Helper()
	const sourceRate = 44100
	samples := make([]int16, sourceRate)
	for i := range samples {
		samples[i] = int16((i%200 - 100) * 120)
	}
	var out bytes.Buffer
	enc := shinemp3.NewEncoder(sourceRate, 1)
	if err := enc.Write(&out, samples); err != nil {
		t.Fatalf("encode test mp3: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("encoded test mp3 empty")
	}
	return out.Bytes()
}

func testWAVPCM(audioFormat, channels, sampleRate, bitsPerSample int, pcm []byte) []byte {
	byteRate := sampleRate * channels * (bitsPerSample / 8)
	blockAlign := channels * (bitsPerSample / 8)
	dataSize := len(pcm)
	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	putU32LE(buf[4:8], uint32(36+dataSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	putU32LE(buf[16:20], 16)
	putU16LE(buf[20:22], uint16(audioFormat))
	putU16LE(buf[22:24], uint16(channels))
	putU32LE(buf[24:28], uint32(sampleRate))
	putU32LE(buf[28:32], uint32(byteRate))
	putU16LE(buf[32:34], uint16(blockAlign))
	putU16LE(buf[34:36], uint16(bitsPerSample))
	copy(buf[36:40], "data")
	putU32LE(buf[40:44], uint32(dataSize))
	copy(buf[44:], pcm)
	return buf
}

func testOggPage(seq uint32, packet []byte) []byte {
	return testOggPageWithHeader(seq, 0, packet)
}

func testOggPageWithHeader(seq uint32, headerType byte, packet []byte) []byte {
	lacing := make([]byte, 0, (len(packet)/255)+1)
	for remaining := len(packet); remaining >= 255; remaining -= 255 {
		lacing = append(lacing, 255)
	}
	if len(packet)%255 != 0 {
		lacing = append(lacing, byte(len(packet)%255))
	}
	if len(packet) == 0 {
		lacing = append(lacing, 0)
	}
	buf := make([]byte, 27+len(lacing)+len(packet))
	copy(buf[0:4], "OggS")
	buf[4] = 0
	buf[5] = headerType
	putU32LE(buf[18:22], seq)
	buf[26] = byte(len(lacing))
	copy(buf[27:27+len(lacing)], lacing)
	copy(buf[27+len(lacing):], packet)
	return buf
}

func testWAVPCMHeaderOnly(channels, sampleRate, bitsPerSample int) []byte {
	byteRate := sampleRate * channels * (bitsPerSample / 8)
	blockAlign := channels * (bitsPerSample / 8)
	buf := make([]byte, 44)
	copy(buf[0:4], "RIFF")
	putU32LE(buf[4:8], 36)
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	putU32LE(buf[16:20], 16)
	putU16LE(buf[20:22], 1)
	putU16LE(buf[22:24], uint16(channels))
	putU32LE(buf[24:28], uint32(sampleRate))
	putU32LE(buf[28:32], uint32(byteRate))
	putU16LE(buf[32:34], uint16(blockAlign))
	putU16LE(buf[34:36], uint16(bitsPerSample))
	copy(buf[36:40], "JUNK")
	putU32LE(buf[40:44], 0)
	return buf
}

func testWAVWithOddJunkMissingPadding() []byte {
	buf := testWAVPCMHeaderOnly(1, TargetSampleRate, 16)
	buf = append(buf, []byte{'J', 'U', 'N', 'K', 1, 0, 0, 0, 'x'}...)
	putU32LE(buf[4:8], uint32(len(buf)-8))
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
