package guiapp

import (
	"encoding/binary"
	"math"

	"github.com/RapidAI/CodeClaw/guiapp/petpack"
)

// writeDutyWAV renders host cue tones into a 16-bit mono WAV. macOS plays
// that file off the main thread. Windows uses the kernel Beep path instead.
func writeDutyWAV(tones []petpack.RigTone, pitch float64) []byte {
	const rate = 22050
	if pitch <= 0 {
		pitch = 1
	}
	samples := make([]int16, 0, rate)
	for index, tone := range tones {
		count := rate * tone.Ms / 1000
		if count < 1 {
			count = 1
		}
		freq := float64(tone.Hz) * pitch
		for sample := 0; sample < count; sample++ {
			env := 1.0
			if sample < 80 {
				env = float64(sample) / 80
			}
			if remain := count - sample; remain < 80 {
				fade := float64(remain) / 80
				if fade < env {
					env = fade
				}
			}
			value := math.Sin(2*math.Pi*freq*float64(sample)/float64(rate)) * 0.35 * env
			samples = append(samples, int16(value*32767))
		}
		if index+1 < len(tones) {
			samples = append(samples, make([]int16, rate*8/1000)...)
		}
	}
	dataBytes := len(samples) * 2
	buf := make([]byte, 44+dataBytes)
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+dataBytes))
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], 1)
	binary.LittleEndian.PutUint32(buf[24:], rate)
	binary.LittleEndian.PutUint32(buf[28:], rate*2)
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(dataBytes))
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(buf[44+i*2:], uint16(sample))
	}
	return buf
}
