// corelib/audioconv/opus.go — OGG/Opus to WAV converter.
//
// Uses github.com/pion/opus (pure Go) for Opus decoding and the local
// OGG demuxer to extract packets. Feishu and Telegram voice messages
// use OGG/Opus encoding.
package audioconv

import (
	"fmt"

	"github.com/pion/opus"
)

// opusToWAV decodes OGG/Opus audio data to 16kHz mono 16-bit WAV.
func opusToWAV(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("audioconv: empty opus data")
	}

	// Extract Opus packets from OGG container.
	packets, err := extractOggOpusPackets(data)
	if err != nil {
		return nil, fmt.Errorf("audioconv: ogg demux failed: %w", err)
	}

	// Decode each Opus packet to S16LE PCM.
	// Opus internally always operates at 48kHz; pion/opus decodes at the
	// bandwidth's effective sample rate (returned per-packet).
	decoder := opus.NewDecoder()

	// Max frame: 60ms at 48kHz = 2880 samples × 2 bytes = 5760 bytes.
	const maxFrameBytes = 5760
	frameBuf := make([]byte, maxFrameBytes)

	var allPCM []byte
	decodeSampleRate := 0 // determined from first successful decode

	for _, pkt := range packets {
		// Split code=3 multi-frame packets into individual frames
		// since pion/opus doesn't support code=3.
		subPkts := splitOpusPacket(pkt)
		for _, sp := range subPkts {
			// Rewrite Hybrid mode TOC to SILK-only so pion/opus can decode.
			// Hybrid packets contain SILK + CELT data; SILK decoder will
			// read only what it needs and ignore trailing CELT bytes.
			sp = rewriteHybridToSilk(sp)
			bw, _, err := decoder.Decode(sp, frameBuf)
			if err != nil {
				continue
			}
			pktRate := bw.SampleRate()
			if decodeSampleRate == 0 {
				decodeSampleRate = pktRate
			}
			n := opusFrameBytes(sp, pktRate)
			if n > 0 && n <= len(frameBuf) {
				allPCM = append(allPCM, frameBuf[:n]...)
			}
		}
	}

	if len(allPCM) == 0 {
		return nil, fmt.Errorf("audioconv: opus decode produced empty PCM")
	}
	if decodeSampleRate == 0 {
		decodeSampleRate = 48000
	}

	// Resample to 16kHz if needed.
	if decodeSampleRate != TargetSampleRate {
		allPCM = resampleS16(allPCM, decodeSampleRate, TargetSampleRate)
	}

	return pcmToWAV(allPCM, TargetSampleRate, TargetChannels)
}


// opusFrameBytes estimates the decoded PCM byte count for an Opus packet
// based on its TOC byte. Returns bytes (S16LE mono).
func opusFrameBytes(pkt []byte, sampleRate int) int {
	if len(pkt) == 0 {
		return 0
	}
	toc := pkt[0]
	config := (toc >> 3) & 0x1F

	// Frame duration in microseconds based on config number.
	var frameDurUs int
	switch {
	case config <= 11:
		// SILK-only: configs 0-3 = 10,20,40,60ms; 4-7 same; 8-11 same
		switch config % 4 {
		case 0:
			frameDurUs = 10000
		case 1:
			frameDurUs = 20000
		case 2:
			frameDurUs = 40000
		case 3:
			frameDurUs = 60000
		}
	case config <= 15:
		// Hybrid: 12-13 = 10,20ms; 14-15 = 10,20ms
		if config%2 == 0 {
			frameDurUs = 10000
		} else {
			frameDurUs = 20000
		}
	default:
		// CELT-only: 16-19, 20-23, 24-27, 28-31 = 2.5,5,10,20ms
		switch config % 4 {
		case 0:
			frameDurUs = 2500
		case 1:
			frameDurUs = 5000
		case 2:
			frameDurUs = 10000
		case 3:
			frameDurUs = 20000
		}
	}

	// Number of frames per packet (from code field, bits 0-1 of TOC).
	code := toc & 0x03
	numFrames := 1
	switch code {
	case 0:
		numFrames = 1
	case 1, 2:
		numFrames = 2
	case 3:
		// Arbitrary number of frames — read from next byte.
		if len(pkt) > 1 {
			numFrames = int(pkt[1] & 0x3F)
		}
	}

	samples := sampleRate * frameDurUs / 1000000 * numFrames
	return samples * 2 // S16LE = 2 bytes per sample
}

// splitOpusPacket splits a multi-frame Opus packet (code 1,2,3) into
// individual code=0 single-frame packets that pion/opus can decode.
// For code=0 packets, returns the packet as-is.
// RFC 6716 §3.2 defines the packet structure.
func splitOpusPacket(pkt []byte) [][]byte {
	if len(pkt) < 1 {
		return nil
	}
	toc := pkt[0]
	code := toc & 0x03

	if code == 0 {
		// Single frame — return as-is.
		return [][]byte{pkt}
	}

	// Build a code=0 TOC byte (same config+stereo, code=0).
	toc0 := toc & 0xFC // clear code bits

	if code == 1 {
		// Two equal-size frames. Each frame = (len(pkt)-1) / 2 bytes.
		payload := pkt[1:]
		if len(payload) < 2 {
			return [][]byte{pkt}
		}
		frameSize := len(payload) / 2
		return [][]byte{
			append([]byte{toc0}, payload[:frameSize]...),
			append([]byte{toc0}, payload[frameSize:frameSize*2]...),
		}
	}

	if code == 2 {
		// Two frames, sizes given by first frame's self-delimiting length.
		payload := pkt[1:]
		sz1, hdrLen := readOpusFrameSize(payload)
		if hdrLen == 0 || sz1 > len(payload)-hdrLen {
			return [][]byte{pkt}
		}
		f1 := payload[hdrLen : hdrLen+sz1]
		f2 := payload[hdrLen+sz1:]
		return [][]byte{
			append([]byte{toc0}, f1...),
			append([]byte{toc0}, f2...),
		}
	}

	// code == 3: arbitrary number of frames.
	if len(pkt) < 2 {
		return [][]byte{pkt}
	}
	fcByte := pkt[1]
	M := int(fcByte & 0x3F)
	vbr := (fcByte & 0x80) != 0
	hasPad := (fcByte & 0x40) != 0

	if M == 0 {
		return nil
	}

	pos := 2

	// Skip padding bytes
	if hasPad {
		padTotal := 0
		for pos < len(pkt) {
			b := int(pkt[pos])
			pos++
			padTotal += b
			if b < 255 {
				break
			}
		}
		_ = padTotal // padding is at the end, we just skip the length bytes
	}

	if vbr {
		// VBR: M-1 frame sizes, then last frame is remainder.
		sizes := make([]int, M)
		consumed := 0
		for i := 0; i < M-1; i++ {
			sz, hdrLen := readOpusFrameSize(pkt[pos:])
			if hdrLen == 0 {
				return [][]byte{pkt} // fallback
			}
			sizes[i] = sz
			consumed += sz
			pos += hdrLen
		}
		remaining := len(pkt) - pos - consumed
		if remaining < 0 {
			return [][]byte{pkt}
		}
		sizes[M-1] = remaining

		// Extract frames
		dataStart := pos
		result := make([][]byte, 0, M)
		for i := 0; i < M; i++ {
			end := dataStart + sizes[i]
			if end > len(pkt) {
				break
			}
			frame := pkt[dataStart:end]
			result = append(result, append([]byte{toc0}, frame...))
			dataStart = end
		}
		return result
	}

	// CBR: all frames equal size.
	dataLen := len(pkt) - pos
	if dataLen <= 0 || M == 0 {
		return nil
	}
	frameSize := dataLen / M
	result := make([][]byte, 0, M)
	for i := 0; i < M; i++ {
		start := pos + i*frameSize
		end := start + frameSize
		if end > len(pkt) {
			break
		}
		result = append(result, append([]byte{toc0}, pkt[start:end]...))
	}
	return result
}

// readOpusFrameSize reads a self-delimiting frame size (RFC 6716 §3.2.1).
// Returns (size, bytesConsumed). bytesConsumed=0 on error.
func readOpusFrameSize(data []byte) (int, int) {
	if len(data) == 0 {
		return 0, 0
	}
	if data[0] < 252 {
		return int(data[0]), 1
	}
	if len(data) < 2 {
		return 0, 0
	}
	return int(data[0]) + int(data[1])*4, 2
}

// rewriteHybridToSilk rewrites a Hybrid-mode Opus packet's TOC byte to
// the equivalent SILK-only config so pion/opus (which only supports SILK)
// can decode it. The SILK decoder reads only the SILK portion of the
// bitstream via range coding and ignores trailing CELT data.
//
// Hybrid config mapping (RFC 6716 Table 2):
//   Config 12 (Hybrid SWB 10ms) → Config 4 (SILK MB 10ms)
//   Config 13 (Hybrid SWB 20ms) → Config 5 (SILK MB 20ms)
//   Config 14 (Hybrid FB 10ms)  → Config 8 (SILK WB 10ms)
//   Config 15 (Hybrid FB 20ms)  → Config 9 (SILK WB 20ms)
func rewriteHybridToSilk(pkt []byte) []byte {
	if len(pkt) == 0 {
		return pkt
	}
	toc := pkt[0]
	config := (toc >> 3) & 0x1F
	if config < 12 || config > 15 {
		return pkt // not Hybrid, no rewrite needed
	}

	var silkConfig byte
	switch config {
	case 12:
		silkConfig = 4
	case 13:
		silkConfig = 5
	case 14:
		silkConfig = 8
	case 15:
		silkConfig = 9
	}

	// Rebuild TOC: config(5 bits) | stereo(1 bit) | code(2 bits)
	newTOC := (silkConfig << 3) | (toc & 0x07)
	out := make([]byte, len(pkt))
	copy(out, pkt)
	out[0] = newTOC
	return out
}
