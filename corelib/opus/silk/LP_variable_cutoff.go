package silk

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func silk_LP_interpolate_filter_taps(B_Q28 [3]int32, A_Q28 [2]int32, ind int, fac_Q16 int32) {
	var (
		nb int
		na int
	)
	if ind < int(TRANSITION_INT_NUM-1) {
		if int(fac_Q16) > 0 {
			if int(fac_Q16) < 32768 {
				for nb = 0; nb < TRANSITION_NB; nb++ {
					B_Q28[nb] = int32(int(silk_Transition_LP_B_Q28[ind][nb]) + (((int(silk_Transition_LP_B_Q28[ind+1][nb]) - int(silk_Transition_LP_B_Q28[ind][nb])) * int(int64(int16(fac_Q16)))) >> 16))
				}
				for na = 0; na < TRANSITION_NA; na++ {
					A_Q28[na] = int32(int(silk_Transition_LP_A_Q28[ind][na]) + (((int(silk_Transition_LP_A_Q28[ind+1][na]) - int(silk_Transition_LP_A_Q28[ind][na])) * int(int64(int16(fac_Q16)))) >> 16))
				}
			} else {
				for nb = 0; nb < TRANSITION_NB; nb++ {
					B_Q28[nb] = int32(int(silk_Transition_LP_B_Q28[ind+1][nb]) + (((int(silk_Transition_LP_B_Q28[ind+1][nb]) - int(silk_Transition_LP_B_Q28[ind][nb])) * int(int64(int16(int(fac_Q16)-(1<<16))))) >> 16))
				}
				for na = 0; na < TRANSITION_NA; na++ {
					A_Q28[na] = int32(int(silk_Transition_LP_A_Q28[ind+1][na]) + (((int(silk_Transition_LP_A_Q28[ind+1][na]) - int(silk_Transition_LP_A_Q28[ind][na])) * int(int64(int16(int(fac_Q16)-(1<<16))))) >> 16))
				}
			}
		} else {
			libc.MemCpy(unsafe.Pointer(&B_Q28[0]), unsafe.Pointer(&(silk_Transition_LP_B_Q28[ind])[0]), int(TRANSITION_NB*unsafe.Sizeof(int32(0))))
			libc.MemCpy(unsafe.Pointer(&A_Q28[0]), unsafe.Pointer(&(silk_Transition_LP_A_Q28[ind])[0]), int(TRANSITION_NA*unsafe.Sizeof(int32(0))))
		}
	} else {
		libc.MemCpy(unsafe.Pointer(&B_Q28[0]), unsafe.Pointer(&(silk_Transition_LP_B_Q28[int(TRANSITION_INT_NUM-1)])[0]), int(TRANSITION_NB*unsafe.Sizeof(int32(0))))
		libc.MemCpy(unsafe.Pointer(&A_Q28[0]), unsafe.Pointer(&(silk_Transition_LP_A_Q28[int(TRANSITION_INT_NUM-1)])[0]), int(TRANSITION_NA*unsafe.Sizeof(int32(0))))
	}
}
func LP_variable_cutoff(psLP *LPState, frame []int16, frame_length int) {
	var (
		B_Q28   [3]int32
		A_Q28   [2]int32
		fac_Q16 int32 = 0
		ind     int   = 0
	)
	if psLP.Mode != 0 {
		fac_Q16 = int32(int(uint32(int32((TRANSITION_TIME_MS/(int(SUB_FRAME_LENGTH_MS*MAX_NB_SUBFR)))-int(psLP.Transition_frame_no)))) << (16 - 6))
		ind = int(fac_Q16) >> 16
		fac_Q16 -= int32(int(uint32(int32(ind))) << 16)
		silk_LP_interpolate_filter_taps(B_Q28, A_Q28, ind, fac_Q16)
		if 0 > (TRANSITION_TIME_MS / (int(SUB_FRAME_LENGTH_MS * MAX_NB_SUBFR))) {
			if (int(psLP.Transition_frame_no) + psLP.Mode) > 0 {
				psLP.Transition_frame_no = 0
			} else if (int(psLP.Transition_frame_no) + psLP.Mode) < (TRANSITION_TIME_MS / (int(SUB_FRAME_LENGTH_MS * MAX_NB_SUBFR))) {
				psLP.Transition_frame_no = int32(TRANSITION_TIME_MS / (int(SUB_FRAME_LENGTH_MS * MAX_NB_SUBFR)))
			} else {
				psLP.Transition_frame_no = int32(int(psLP.Transition_frame_no) + psLP.Mode)
			}
		} else if (int(psLP.Transition_frame_no) + psLP.Mode) > (TRANSITION_TIME_MS / (int(SUB_FRAME_LENGTH_MS * MAX_NB_SUBFR))) {
			psLP.Transition_frame_no = int32(TRANSITION_TIME_MS / (int(SUB_FRAME_LENGTH_MS * MAX_NB_SUBFR)))
		} else if (int(psLP.Transition_frame_no) + psLP.Mode) < 0 {
			psLP.Transition_frame_no = 0
		} else {
			psLP.Transition_frame_no = int32(int(psLP.Transition_frame_no) + psLP.Mode)
		}
		silk_biquad_alt_stride1(frame, B_Q28[:], A_Q28[:], psLP.In_LP_State[:], frame, int32(frame_length))
	}
}
