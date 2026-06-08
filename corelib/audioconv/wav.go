// corelib/audioconv/wav.go — WAV header writer and PCM resampler.
package audioconv

import (
	"encoding/binary"
	"fmt"
)

// Target format for ASR: 16kHz mono 16-bit signed little-endian PCM.
const (
	TargetSampleRate  = 16000
	TargetChannels    = 1
	TargetBitsPerSamp = 16
)

// pcmToWAV wraps raw S16LE PCM samples in a WAV container.
// sampleRate and channels describe the PCM data; no resampling is done here.
func pcmToWAV(pcm []byte, sampleRate, channels int) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, fmt.Errorf("audioconv: empty PCM data")
	}
	bitsPerSample := TargetBitsPerSamp
	byteRate := sampleRate * channels * (bitsPerSample / 8)
	blockAlign := channels * (bitsPerSample / 8)
	dataSize := len(pcm)
	fileSize := 36 + dataSize // 44 - 8

	buf := make([]byte, 44+dataSize)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(fileSize))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // PCM format
	binary.LittleEndian.PutUint16(buf[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], uint16(bitsPerSample))
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataSize))
	copy(buf[44:], pcm)
	return buf, nil
}

// resampleS16 resamples mono S16LE PCM from srcRate to dstRate using linear
// interpolation. Good enough for speech; avoids any external dependency.
func resampleS16(pcm []byte, srcRate, dstRate int) []byte {
	if srcRate <= 0 || dstRate <= 0 {
		return nil
	}
	if srcRate == dstRate {
		return pcm
	}
	srcSamples := len(pcm) / 2
	if srcSamples == 0 {
		return pcm
	}
	dstSamples := int(int64(srcSamples) * int64(dstRate) / int64(srcRate))
	out := make([]byte, dstSamples*2)

	ratio := float64(srcRate) / float64(dstRate)
	for i := 0; i < dstSamples; i++ {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := srcPos - float64(idx)

		s0 := readS16LE(pcm, idx)
		s1 := s0
		if idx+1 < srcSamples {
			s1 = readS16LE(pcm, idx+1)
		}
		val := float64(s0)*(1-frac) + float64(s1)*frac
		writeS16LE(out, i, int16(val))
	}
	return out
}

func readS16LE(buf []byte, idx int) int16 {
	off := idx * 2
	return int16(binary.LittleEndian.Uint16(buf[off : off+2]))
}

func writeS16LE(buf []byte, idx int, val int16) {
	off := idx * 2
	binary.LittleEndian.PutUint16(buf[off:off+2], uint16(val))
}
