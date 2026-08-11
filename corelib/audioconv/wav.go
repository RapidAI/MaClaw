// corelib/audioconv/wav.go — WAV header writer and PCM resampler.
package audioconv

import (
	"encoding/binary"
	"fmt"
	"math"
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

// resampleS16 resamples mono S16LE PCM from srcRate to dstRate through a
// Blackman-windowed sinc low-pass filter. Plain linear interpolation has no
// anti-aliasing, so downsampling 24/44.1/48 kHz TTS audio folds content above
// the destination Nyquist back into the audible band — on the ESP32 speaker
// that shows up as broadband hiss behind otherwise intelligible speech.
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

	// Cutoff at 92% of the destination Nyquist (in input-sample units). When
	// upsampling, the input Nyquist is the limit instead.
	cutoff := 0.46 * math.Min(1.0, float64(dstRate)/float64(srcRate))
	// Kernel half-width in input samples; 129 taps with a Blackman window
	// narrows the transition band so content just above the destination
	// Nyquist still lands deep in the stopband, and stays cheap for short
	// clips.
	const halfWidth = 64
	ratio := float64(srcRate) / float64(dstRate)
	for i := 0; i < dstSamples; i++ {
		center := float64(i) * ratio
		lo := int(math.Ceil(center - halfWidth))
		hi := int(math.Floor(center + halfWidth))
		sum := 0.0
		norm := 0.0
		for k := lo; k <= hi; k++ {
			if k < 0 || k >= srcSamples {
				continue
			}
			x := center - float64(k)
			w := 0.42 + 0.5*math.Cos(math.Pi*x/halfWidth) + 0.08*math.Cos(2*math.Pi*x/halfWidth)
			var h float64
			if math.Abs(x) < 1e-9 {
				h = 2 * cutoff
			} else {
				h = math.Sin(2*math.Pi*cutoff*x) / (math.Pi * x)
			}
			weight := h * w
			sum += float64(readS16LE(pcm, k)) * weight
			norm += weight
		}
		if norm != 0 {
			sum /= norm
		}
		if sum > math.MaxInt16 {
			sum = math.MaxInt16
		} else if sum < math.MinInt16 {
			sum = math.MinInt16
		}
		writeS16LE(out, i, int16(math.RoundToEven(sum)))
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
