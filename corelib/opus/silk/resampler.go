package silk

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

const USE_silk_resampler_copy = 0
const USE_silk_resampler_private_up2_HQ_wrapper = 1
const USE_silk_resampler_private_IIR_FIR = 2
const USE_silk_resampler_private_down_FIR = 3

var delay_matrix_enc [5][3]int8 = [5][3]int8{{6, 0, 3}, {0, 7, 3}, {0, 1, 10}, {0, 2, 6}, {18, 10, 12}}
var delay_matrix_dec [3][5]int8 = [3][5]int8{{4, 0, 2, 0, 0}, {0, 9, 4, 7, 4}, {0, 3, 12, 7, 7}}

func (S *ResamplerState) Init(Fs_Hz_in int32, Fs_Hz_out int32, forEnc int) int {
	var up2x int
	*S = ResamplerState{}
	if forEnc != 0 {
		if int(Fs_Hz_in) != 8000 && int(Fs_Hz_in) != 12000 && int(Fs_Hz_in) != 16000 && int(Fs_Hz_in) != 24000 && int(Fs_Hz_in) != 48000 || int(Fs_Hz_out) != 8000 && int(Fs_Hz_out) != 12000 && int(Fs_Hz_out) != 16000 {
			return -1
		}
		S.InputDelay = int(delay_matrix_enc[(((int(Fs_Hz_in)>>12)-int(libc.BoolToInt(int(Fs_Hz_in) > 16000)))>>int(libc.BoolToInt(int(Fs_Hz_in) > 24000)))-1][(((int(Fs_Hz_out)>>12)-int(libc.BoolToInt(int(Fs_Hz_out) > 16000)))>>int(libc.BoolToInt(int(Fs_Hz_out) > 24000)))-1])
	} else {
		if int(Fs_Hz_in) != 8000 && int(Fs_Hz_in) != 12000 && int(Fs_Hz_in) != 16000 || int(Fs_Hz_out) != 8000 && int(Fs_Hz_out) != 12000 && int(Fs_Hz_out) != 16000 && int(Fs_Hz_out) != 24000 && int(Fs_Hz_out) != 48000 {
			return -1
		}
		S.InputDelay = int(delay_matrix_dec[(((int(Fs_Hz_in)>>12)-int(libc.BoolToInt(int(Fs_Hz_in) > 16000)))>>int(libc.BoolToInt(int(Fs_Hz_in) > 24000)))-1][(((int(Fs_Hz_out)>>12)-int(libc.BoolToInt(int(Fs_Hz_out) > 16000)))>>int(libc.BoolToInt(int(Fs_Hz_out) > 24000)))-1])
	}
	S.Fs_in_kHz = int(int32(int(Fs_Hz_in) / 1000))
	S.Fs_out_kHz = int(int32(int(Fs_Hz_out) / 1000))
	S.BatchSize = S.Fs_in_kHz * RESAMPLER_MAX_BATCH_SIZE_MS
	up2x = 0
	if int(Fs_Hz_out) > int(Fs_Hz_in) {
		if int(Fs_Hz_out) == (int(Fs_Hz_in) * 2) {
			S.Resampler_function = 1
		} else {
			S.Resampler_function = 2
			up2x = 1
		}
	} else if int(Fs_Hz_out) < int(Fs_Hz_in) {
		S.Resampler_function = 3
		if (int(Fs_Hz_out) * 4) == (int(Fs_Hz_in) * 3) {
			S.FIR_Fracs = 3
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR0
			S.Coefs = silk_Resampler_3_4_COEFS[:][:]
		} else if (int(Fs_Hz_out) * 3) == (int(Fs_Hz_in) * 2) {
			S.FIR_Fracs = 2
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR0
			S.Coefs = silk_Resampler_2_3_COEFS[:][:]
		} else if (int(Fs_Hz_out) * 2) == int(Fs_Hz_in) {
			S.FIR_Fracs = 1
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR1
			S.Coefs = silk_Resampler_1_2_COEFS[:][:]
		} else if (int(Fs_Hz_out) * 3) == int(Fs_Hz_in) {
			S.FIR_Fracs = 1
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR2
			S.Coefs = silk_Resampler_1_3_COEFS[:][:]
		} else if (int(Fs_Hz_out) * 4) == int(Fs_Hz_in) {
			S.FIR_Fracs = 1
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR2
			S.Coefs = silk_Resampler_1_4_COEFS[:][:]
		} else if (int(Fs_Hz_out) * 6) == int(Fs_Hz_in) {
			S.FIR_Fracs = 1
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR2
			S.Coefs = silk_Resampler_1_6_COEFS[:][:]
		} else {
			return -1
		}
	} else {
		S.Resampler_function = 0
	}
	S.InvRatio_Q16 = int32(int(uint32(int32(int(int32(int(uint32(Fs_Hz_in))<<(up2x+14)))/int(Fs_Hz_out)))) << 2)
	for int(int32((int64(S.InvRatio_Q16)*int64(Fs_Hz_out))>>16)) < int(int32(int(uint32(Fs_Hz_in))<<up2x)) {
		S.InvRatio_Q16++
	}
	return 0
}
func (S *ResamplerState) Resample(out []int16, in []int16, inLen int32) int {
	nSamples := S.Fs_in_kHz - S.InputDelay
	libc.MemCpy(unsafe.Pointer(&S.DelayBuf[S.InputDelay]), unsafe.Pointer(&in[0]), nSamples*int(unsafe.Sizeof(int16(0))))
	switch S.Resampler_function {
	case 1:
		ResamplerPrivateUp2HQWrapper(S, out, S.DelayBuf[:], int32(S.Fs_in_kHz))
		ResamplerPrivateUp2HQWrapper(S, out[S.Fs_out_kHz:], in[nSamples:], int32(int(inLen)-S.Fs_in_kHz))
	case 2:
		ResamplerPrivateIIR_FIR(S, out, S.DelayBuf[:], int32(S.Fs_in_kHz))
		ResamplerPrivateIIR_FIR(S, out[S.Fs_out_kHz:], in[nSamples:], int32(int(inLen)-S.Fs_in_kHz))
	case 3:
		ResamplerPrivateDownFIR(S, out, S.DelayBuf[:], int32(S.Fs_in_kHz))
		ResamplerPrivateDownFIR(S, out[S.Fs_out_kHz:], in[nSamples:], int32(int(inLen)-S.Fs_in_kHz))
	default:
		libc.MemCpy(unsafe.Pointer(&out[0]), unsafe.Pointer(&S.DelayBuf[0]), S.Fs_in_kHz*int(unsafe.Sizeof(int16(0))))
		libc.MemCpy(unsafe.Pointer(&out[S.Fs_out_kHz]), unsafe.Pointer(&in[nSamples]), (int(inLen)-S.Fs_in_kHz)*int(unsafe.Sizeof(int16(0))))
	}
	libc.MemCpy(unsafe.Pointer(&S.DelayBuf[0]), unsafe.Pointer(&in[int(inLen)-S.InputDelay]), S.InputDelay*int(unsafe.Sizeof(int16(0))))
	return 0
}
