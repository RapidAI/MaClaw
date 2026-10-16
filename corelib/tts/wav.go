package tts

import "math"

// EncodeWAV converts float32 PCM [-1,1] to a WAV file byte slice.
func EncodeWAV(samples []float32, sampleRate int) []byte {
	nSamples := len(samples)
	dataSize := nSamples * 2 // 16-bit PCM
	fileSize := 44 + dataSize

	buf := make([]byte, fileSize)
	copy(buf[0:4], "RIFF")
	le32(buf[4:], uint32(fileSize-8))
	copy(buf[8:12], "WAVE")

	// fmt chunk
	copy(buf[12:16], "fmt ")
	le32(buf[16:], 16) // chunk size
	le16(buf[20:], 1)  // PCM format
	le16(buf[22:], 1)  // mono
	le32(buf[24:], uint32(sampleRate))
	le32(buf[28:], uint32(sampleRate*2)) // byte rate
	le16(buf[32:], 2)                    // block align
	le16(buf[34:], 16)                   // bits per sample

	// data chunk
	copy(buf[36:40], "data")
	le32(buf[40:], uint32(dataSize))

	for i, s := range samples {
		// Clamp to [-1, 1]
		if s > 1 {
			s = 1
		} else if s < -1 {
			s = -1
		}
		v := int16(math.Round(float64(s) * 32767))
		le16(buf[44+i*2:], uint16(v))
	}
	return buf
}

func le32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func le16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}
