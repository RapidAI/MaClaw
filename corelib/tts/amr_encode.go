package tts

import (
	"encoding/binary"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/amrnb"
)

// EncodeWAVToAMR converts PCM WAV audio to AMR-NB for WeCom native voice.
func EncodeWAVToAMR(wavData []byte) ([]byte, error) {
	pcm, sampleRate, channels, err := parseWAVS16(wavData)
	if err != nil {
		return nil, fmt.Errorf("parse WAV: %w", err)
	}
	if channels == 2 {
		pcm = downmixStereoS16(pcm)
		channels = 1
	}
	if channels != 1 {
		return nil, fmt.Errorf("unsupported channel count: %d", channels)
	}
	if sampleRate != amrnb.SampleRate {
		pcm = resampleS16Samples(pcm, sampleRate, amrnb.SampleRate)
	}
	return amrnb.EncodeS16(pcm)
}

func parseWAVS16(data []byte) ([]int16, int, int, error) {
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, 0, fmt.Errorf("not a valid WAV file")
	}
	pos := 12
	var sampleRate, channels, bitsPerSample, audioFormat int
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		sz := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		chunkStart := pos + 8
		chunkEnd := chunkStart + sz
		if chunkEnd > len(data) {
			chunkEnd = len(data)
		}
		if id == "fmt " && sz >= 16 && chunkStart+16 <= len(data) {
			audioFormat = int(binary.LittleEndian.Uint16(data[chunkStart : chunkStart+2]))
			channels = int(binary.LittleEndian.Uint16(data[chunkStart+2 : chunkStart+4]))
			sampleRate = int(binary.LittleEndian.Uint32(data[chunkStart+4 : chunkStart+8]))
			bitsPerSample = int(binary.LittleEndian.Uint16(data[chunkStart+14 : chunkStart+16]))
		} else if id == "data" {
			if audioFormat != 1 || sampleRate == 0 || channels == 0 || bitsPerSample != 16 {
				return nil, 0, 0, fmt.Errorf("WAV must be 16-bit PCM")
			}
			raw := data[chunkStart:chunkEnd]
			pcm := make([]int16, len(raw)/2)
			for i := range pcm {
				pcm[i] = int16(binary.LittleEndian.Uint16(raw[i*2:]))
			}
			return pcm, sampleRate, channels, nil
		}
		pos += 8 + sz
		if sz%2 != 0 {
			pos++
		}
	}
	return nil, 0, 0, fmt.Errorf("WAV data chunk not found")
}

func downmixStereoS16(in []int16) []int16 {
	out := make([]int16, len(in)/2)
	for i := range out {
		out[i] = int16((int(in[i*2]) + int(in[i*2+1])) / 2)
	}
	return out
}

func resampleS16Samples(in []int16, srcRate, dstRate int) []int16 {
	if srcRate == dstRate || len(in) == 0 {
		return in
	}
	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(float64(len(in)) / ratio)
	if outLen <= 0 {
		outLen = 1
	}
	out := make([]int16, outLen)
	for i := range out {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := srcPos - float64(idx)
		s0 := float64(in[idx])
		s1 := s0
		if idx+1 < len(in) {
			s1 = float64(in[idx+1])
		}
		out[i] = int16(s0*(1-frac) + s1*frac)
	}
	return out
}
