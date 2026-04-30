package tts

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/libopus"
)

// EncodeWAVToOpus converts WAV audio data to OGG Opus format using the
// built-in pure Go Opus encoder (forked from gotranspile/opus).
// Zero external dependencies — no ffmpeg, no CGo, no libopus C library.
//
// The output is suitable for:
//   - Feishu: file_type=opus upload + msg_type=audio
//   - DingTalk: sampleAudio (ogg format)
//   - Telegram: sendVoice (OGG Opus)
func EncodeWAVToOpus(wavData []byte) ([]byte, error) {
	pcm, sampleRate, channels, err := parseWAVForOpus(wavData)
	if err != nil {
		return nil, fmt.Errorf("parse WAV: %w", err)
	}

	// Opus requires specific sample rates. TTS outputs 22050 Hz — resample to 48000.
	if sampleRate != 48000 && sampleRate != 24000 && sampleRate != 16000 &&
		sampleRate != 12000 && sampleRate != 8000 {
		pcm = resampleFloat32(pcm, sampleRate, 48000)
		sampleRate = 48000
	}

	var errCode int
	enc := libopus.OpusEncoderCreate(int32(sampleRate), channels, libopus.OPUS_APPLICATION_VOIP, &errCode)
	if errCode != 0 || enc == nil {
		return nil, fmt.Errorf("opus encoder create failed: error code %d", errCode)
	}
	defer libopus.OpusEncoderDestroy(enc)

	libopus.OpusEncoderCtl(enc, libopus.OPUS_SET_BITRATE_REQUEST, int32(32000))

	frameSize := sampleRate * 20 / 1000 // 960 at 48kHz
	maxPacketSize := 4000

	var opusPackets [][]byte
	var granulePos uint64
	outBuf := make([]byte, maxPacketSize) // reuse across frames

	for offset := 0; offset+frameSize <= len(pcm); offset += frameSize {
		n := libopus.OpusEncodeFloat(enc,
			(*float32)(unsafe.Pointer(&pcm[offset])),
			frameSize,
			(*uint8)(unsafe.Pointer(&outBuf[0])),
			int32(maxPacketSize),
		)
		if n < 0 {
			return nil, fmt.Errorf("opus encode failed at offset %d: error %d", offset, n)
		}
		pkt := make([]byte, n)
		copy(pkt, outBuf[:n])
		opusPackets = append(opusPackets, pkt)
		granulePos += uint64(frameSize)
	}

	if len(opusPackets) == 0 {
		return nil, fmt.Errorf("no audio frames to encode (input too short)")
	}

	return muxOggOpus(opusPackets, sampleRate, channels, granulePos)
}

// HasOpusEncoder always returns true — built-in pure Go encoder.
func HasOpusEncoder() bool { return true }

// ---------------------------------------------------------------------------
// WAV parsing
// ---------------------------------------------------------------------------

func parseWAVForOpus(data []byte) ([]float32, int, int, error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, 0, fmt.Errorf("not a valid WAV file")
	}
	pos := 12
	var sampleRate, channels, bitsPerSample int
	for pos+8 < len(data) {
		id := string(data[pos : pos+4])
		sz := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		if id == "fmt " && sz >= 16 {
			channels = int(binary.LittleEndian.Uint16(data[pos+10 : pos+12]))
			sampleRate = int(binary.LittleEndian.Uint32(data[pos+12 : pos+16]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[pos+22 : pos+24]))
			pos += 8 + sz
			if sz%2 != 0 { pos++ }
			continue
		}
		if id == "data" {
			raw := data[pos+8:]
			if pos+8+sz <= len(data) { raw = data[pos+8 : pos+8+sz] }
			var pcm []float32
			switch bitsPerSample {
			case 16:
				n := len(raw) / 2
				pcm = make([]float32, n)
				for i := 0; i < n; i++ {
					pcm[i] = float32(int16(binary.LittleEndian.Uint16(raw[i*2:]))) / 32768.0
				}
			case 32:
				n := len(raw) / 4
				pcm = make([]float32, n)
				for i := 0; i < n; i++ {
					bits := binary.LittleEndian.Uint32(raw[i*4:])
					pcm[i] = *(*float32)(unsafe.Pointer(&bits))
				}
			default:
				return nil, 0, 0, fmt.Errorf("unsupported bits per sample: %d", bitsPerSample)
			}
			return pcm, sampleRate, channels, nil
		}
		pos += 8 + sz
		if sz%2 != 0 { pos++ }
	}
	return nil, 0, 0, fmt.Errorf("WAV data chunk not found")
}

func resampleFloat32(in []float32, srcRate, dstRate int) []float32 {
	if srcRate == dstRate { return in }
	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(float64(len(in)) / ratio)
	out := make([]float32, outLen)
	for i := range out {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := float32(srcPos - float64(idx))
		if idx+1 < len(in) {
			out[i] = in[idx]*(1-frac) + in[idx+1]*frac
		} else if idx < len(in) {
			out[i] = in[idx]
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// OGG container muxer (RFC 3533 + RFC 7845)
// ---------------------------------------------------------------------------

func muxOggOpus(packets [][]byte, sampleRate, channels int, totalGranule uint64) ([]byte, error) {
	var buf bytes.Buffer
	serialNo := uint32(0x4F505553)
	var pageSeq uint32

	writeOggPage(&buf, serialNo, pageSeq, 0, 0x02, [][]byte{buildOpusHead(channels, sampleRate)})
	pageSeq++
	writeOggPage(&buf, serialNo, pageSeq, 0, 0x00, [][]byte{buildOpusTags()})
	pageSeq++

	var pagePkts [][]byte
	var pageSize int
	var granule uint64
	frameSamples := uint64(sampleRate * 20 / 1000)

	for i, pkt := range packets {
		granule += frameSamples
		pagePkts = append(pagePkts, pkt)
		pageSize += len(pkt)
		isLast := i == len(packets)-1
		if pageSize >= 3000 || isLast {
			flags := byte(0x00)
			if isLast { flags = 0x04; granule = totalGranule }
			writeOggPage(&buf, serialNo, pageSeq, granule, flags, pagePkts)
			pageSeq++
			pagePkts = nil
			pageSize = 0
		}
	}
	return buf.Bytes(), nil
}

func buildOpusHead(ch, sr int) []byte {
	var b bytes.Buffer
	b.WriteString("OpusHead")
	b.WriteByte(1)
	b.WriteByte(byte(ch))
	binary.Write(&b, binary.LittleEndian, uint16(0))
	binary.Write(&b, binary.LittleEndian, uint32(sr))
	binary.Write(&b, binary.LittleEndian, int16(0))
	b.WriteByte(0)
	return b.Bytes()
}

func buildOpusTags() []byte {
	var b bytes.Buffer
	b.WriteString("OpusTags")
	v := "MacLaw TTS"
	binary.Write(&b, binary.LittleEndian, uint32(len(v)))
	b.WriteString(v)
	binary.Write(&b, binary.LittleEndian, uint32(0))
	return b.Bytes()
}

func writeOggPage(buf *bytes.Buffer, serial, seq uint32, granule uint64, flags byte, segs [][]byte) {
	var segTable []byte
	for _, s := range segs {
		r := len(s)
		for r >= 255 { segTable = append(segTable, 255); r -= 255 }
		segTable = append(segTable, byte(r))
	}
	var hdr bytes.Buffer
	hdr.WriteString("OggS")
	hdr.WriteByte(0)
	hdr.WriteByte(flags)
	binary.Write(&hdr, binary.LittleEndian, granule)
	binary.Write(&hdr, binary.LittleEndian, serial)
	binary.Write(&hdr, binary.LittleEndian, seq)
	binary.Write(&hdr, binary.LittleEndian, uint32(0))
	hdr.WriteByte(byte(len(segTable)))
	hdr.Write(segTable)
	var payload bytes.Buffer
	for _, s := range segs { payload.Write(s) }
	page := append(hdr.Bytes(), payload.Bytes()...)
	binary.LittleEndian.PutUint32(page[22:26], crc32OGG(page))
	buf.Write(page)
}

func crc32OGG(data []byte) uint32 {
	var crc uint32
	for _, b := range data { crc = (crc << 8) ^ oggCRCTable[(crc>>24)^uint32(b)] }
	return crc
}

var oggCRCTable [256]uint32

func init() {
	for i := 0; i < 256; i++ {
		r := uint32(i) << 24
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 { r = (r << 1) ^ 0x04C11DB7 } else { r <<= 1 }
		}
		oggCRCTable[i] = r
	}
}
