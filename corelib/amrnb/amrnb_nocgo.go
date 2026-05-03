//go:build !cgo

package amrnb

import "fmt"

const (
	SampleRate = 8000
	FrameSize  = 160 // 20 ms at 8 kHz
)

// EncodeS16 requires the bundled OpenCORE AMR-NB cgo encoder.
func EncodeS16(pcm []int16) ([]byte, error) {
	return nil, fmt.Errorf("amrnb: encoder unavailable without cgo")
}
