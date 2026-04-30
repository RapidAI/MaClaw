package libopus

import "math"

const MAX_PERIOD = 1024
const BITALLOC_SIZE = 11

type PulseCache struct {
	Size  int
	Index *int16
	Bits  *uint8
	Caps  *uint8
}
type OpusCustomMode struct {
	Fs             int32
	Overlap        int
	NbEBands       int
	EffEBands      int
	Preemph        [4]opus_val16
	EBands         *int16
	MaxLM          int
	NbShortMdcts   int
	ShortMdctSize  int
	NbAllocVectors int
	AllocVectors   *uint8
	LogN           *int16
	Window         *opus_val16
	Mdct           mdct_lookup
	Cache          PulseCache
}

var eband5ms [22]int16 = [22]int16{0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 14, 16, 20, 24, 28, 34, 40, 48, 60, 78, 100}
var band_allocation [231]uint8 = [231]uint8{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 90, 80, 75, 69, 63, 56, 49, 40, 34, 29, 20, 18, 10, 0, 0, 0, 0, 0, 0, 0, 0, 110, 100, 90, 84, 78, 71, 65, 58, 51, 45, 39, 32, 26, 20, 12, 0, 0, 0, 0, 0, 0, 118, 110, 103, 93, 86, 80, 75, 70, 65, 59, 53, 47, 40, 31, 23, 15, 4, 0, 0, 0, 0, 126, 119, 112, 104, 95, 89, 83, 78, 72, 66, 60, 54, 47, 39, 32, 25, 17, 12, 1, 0, 0, 134, math.MaxInt8, 120, 114, 103, 97, 91, 85, 78, 72, 66, 60, 54, 47, 41, 35, 29, 23, 16, 10, 1, 144, 137, 130, 124, 113, 107, 101, 95, 88, 82, 76, 70, 64, 57, 51, 45, 39, 33, 26, 15, 1, 152, 145, 138, 132, 123, 117, 111, 105, 98, 92, 86, 80, 74, 67, 61, 55, 49, 43, 36, 20, 1, 162, 155, 148, 142, 133, math.MaxInt8, 121, 115, 108, 102, 96, 90, 84, 77, 71, 65, 59, 53, 46, 30, 1, 172, 165, 158, 152, 143, 137, 131, 125, 118, 112, 106, 100, 94, 87, 81, 75, 69, 63, 56, 45, 20, 200, 200, 200, 200, 200, 200, 200, 200, 198, 193, 188, 183, 178, 173, 168, 163, 158, 153, 148, 129, 104}

func opus_custom_mode_create(Fs int32, frame_size int, error *int) *OpusCustomMode {
	var i int
	for i = 0; i < TOTAL_MODES; i++ {
		var j int
		for j = 0; j < 4; j++ {
			if int(Fs) == int(static_mode_list[i].Fs) && (frame_size<<j) == static_mode_list[i].ShortMdctSize*static_mode_list[i].NbShortMdcts {
				if error != nil {
					*error = OPUS_OK
				}
				return static_mode_list[i]
			}
		}
	}
	if error != nil {
		*error = -1
	}
	return nil
}
