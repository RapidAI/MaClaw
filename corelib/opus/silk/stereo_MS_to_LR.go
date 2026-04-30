package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func StereoMStoLR(state *StereoDecState, x1 []int16, x2 []int16, pred_Q13 []int32, fs_kHz int, frame_length int) {
	var (
		n          int
		denom_Q16  int
		delta0_Q13 int
		delta1_Q13 int
		sum        int32
		diff       int32
		pred0_Q13  int32
		pred1_Q13  int32
	)
	libc.MemCpy(unsafe.Pointer(&x1[0]), unsafe.Pointer(&state.SMid[0]), int(2*unsafe.Sizeof(int16(0))))
	libc.MemCpy(unsafe.Pointer(&x2[0]), unsafe.Pointer(&state.SSide[0]), int(2*unsafe.Sizeof(int16(0))))
	libc.MemCpy(unsafe.Pointer(&state.SMid[0]), unsafe.Pointer(&x1[frame_length]), int(2*unsafe.Sizeof(int16(0))))
	libc.MemCpy(unsafe.Pointer(&state.SSide[0]), unsafe.Pointer(&x2[frame_length]), int(2*unsafe.Sizeof(int16(0))))
	pred0_Q13 = int32(state.Pred_prev_Q13[0])
	pred1_Q13 = int32(state.Pred_prev_Q13[1])
	denom_Q16 = int(int32((1 << 16) / (STEREO_INTERP_LEN_MS * fs_kHz)))
	if 16 == 1 {
		delta0_Q13 = ((int(int32(int16(int(pred_Q13[0])-int(state.Pred_prev_Q13[0])))) * int(int32(int16(denom_Q16)))) >> 1) + ((int(int32(int16(int(pred_Q13[0])-int(state.Pred_prev_Q13[0])))) * int(int32(int16(denom_Q16)))) & 1)
	} else {
		delta0_Q13 = (((int(int32(int16(int(pred_Q13[0])-int(state.Pred_prev_Q13[0])))) * int(int32(int16(denom_Q16)))) >> (16 - 1)) + 1) >> 1
	}
	if 16 == 1 {
		delta1_Q13 = ((int(int32(int16(int(pred_Q13[1])-int(state.Pred_prev_Q13[1])))) * int(int32(int16(denom_Q16)))) >> 1) + ((int(int32(int16(int(pred_Q13[1])-int(state.Pred_prev_Q13[1])))) * int(int32(int16(denom_Q16)))) & 1)
	} else {
		delta1_Q13 = (((int(int32(int16(int(pred_Q13[1])-int(state.Pred_prev_Q13[1])))) * int(int32(int16(denom_Q16)))) >> (16 - 1)) + 1) >> 1
	}
	for n = 0; n < STEREO_INTERP_LEN_MS*fs_kHz; n++ {
		pred0_Q13 += int32(delta0_Q13)
		pred1_Q13 += int32(delta1_Q13)
		sum = int32(int(uint32(int32((int(x1[n])+int(int32(x1[n+2])))+int(int32(int(uint32(x1[n+1]))<<1))))) << 9)
		sum = int32(int64(int32(int(uint32(int32(x2[n+1])))<<8)) + ((int64(sum) * int64(int16(pred0_Q13))) >> 16))
		sum = int32(int64(sum) + ((int64(int32(int(uint32(int32(x1[n+1])))<<11)) * int64(int16(pred1_Q13))) >> 16))
		if (func() int {
			if 8 == 1 {
				return (int(sum) >> 1) + (int(sum) & 1)
			}
			return ((int(sum) >> (8 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			x2[n+1] = math.MaxInt16
		} else if (func() int {
			if 8 == 1 {
				return (int(sum) >> 1) + (int(sum) & 1)
			}
			return ((int(sum) >> (8 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			x2[n+1] = math.MinInt16
		} else if 8 == 1 {
			x2[n+1] = int16((int(sum) >> 1) + (int(sum) & 1))
		} else {
			x2[n+1] = int16(((int(sum) >> (8 - 1)) + 1) >> 1)
		}
	}
	pred0_Q13 = pred_Q13[0]
	pred1_Q13 = pred_Q13[1]
	for n = STEREO_INTERP_LEN_MS * fs_kHz; n < frame_length; n++ {
		sum = int32(int(uint32(int32((int(x1[n])+int(int32(x1[n+2])))+int(int32(int(uint32(x1[n+1]))<<1))))) << 9)
		sum = int32(int64(int32(int(uint32(int32(x2[n+1])))<<8)) + ((int64(sum) * int64(int16(pred0_Q13))) >> 16))
		sum = int32(int64(sum) + ((int64(int32(int(uint32(int32(x1[n+1])))<<11)) * int64(int16(pred1_Q13))) >> 16))
		if (func() int {
			if 8 == 1 {
				return (int(sum) >> 1) + (int(sum) & 1)
			}
			return ((int(sum) >> (8 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			x2[n+1] = math.MaxInt16
		} else if (func() int {
			if 8 == 1 {
				return (int(sum) >> 1) + (int(sum) & 1)
			}
			return ((int(sum) >> (8 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			x2[n+1] = math.MinInt16
		} else if 8 == 1 {
			x2[n+1] = int16((int(sum) >> 1) + (int(sum) & 1))
		} else {
			x2[n+1] = int16(((int(sum) >> (8 - 1)) + 1) >> 1)
		}
	}
	state.Pred_prev_Q13[0] = int16(pred_Q13[0])
	state.Pred_prev_Q13[1] = int16(pred_Q13[1])
	for n = 0; n < frame_length; n++ {
		sum = int32(int(x1[n+1]) + int(int32(x2[n+1])))
		diff = int32(int(x1[n+1]) - int(int32(x2[n+1])))
		if int(sum) > math.MaxInt16 {
			x1[n+1] = math.MaxInt16
		} else if int(sum) < int(math.MinInt16) {
			x1[n+1] = math.MinInt16
		} else {
			x1[n+1] = int16(sum)
		}
		if int(diff) > math.MaxInt16 {
			x2[n+1] = math.MaxInt16
		} else if int(diff) < int(math.MinInt16) {
			x2[n+1] = math.MinInt16
		} else {
			x2[n+1] = int16(diff)
		}
	}
}
