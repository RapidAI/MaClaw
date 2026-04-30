package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func silk_CNG_exc(exc_Q14 []int32, exc_buf_Q14 []int32, length int, rand_seed *int32) {
	var (
		seed     int32
		idx      int
		exc_mask int
	)
	exc_mask = CNG_BUF_MASK_MAX
	for exc_mask > length {
		exc_mask = exc_mask >> 1
	}
	seed = *rand_seed
	for i := 0; i < length; i++ {
		seed = int32(RAND_INCREMENT + int(uint32(int32(int(uint32(seed))*RAND_MULTIPLIER))))
		idx = (int(seed) >> 24) & exc_mask
		exc_Q14[i] = exc_buf_Q14[idx]
	}
	*rand_seed = seed
}
func CNG_Reset(psDec *DecoderState) {
	NLSF_step_Q15 := int(int32(math.MaxInt16 / (psDec.LPC_order + 1)))
	NLSF_acc_Q15 := 0
	for i := 0; i < psDec.LPC_order; i++ {
		NLSF_acc_Q15 += NLSF_step_Q15
		psDec.SCNG.CNG_smth_NLSF_Q15[i] = int16(NLSF_acc_Q15)
	}
	psDec.SCNG.CNG_smth_Gain_Q16 = 0
	psDec.SCNG.Rand_seed = 3176576
}
func CNG(psDec *DecoderState, psDecCtrl *DecoderControl, frame []int16, length int) {
	var (
		i            int
		subfr        int
		LPC_pred_Q10 int32
		max_Gain_Q16 int32
		gain_Q16     int32
		gain_Q10     int32
		A_Q12        [16]int16
	)
	psCNG := &psDec.SCNG
	if psDec.Fs_kHz != psCNG.Fs_kHz {
		CNG_Reset(psDec)
		psCNG.Fs_kHz = psDec.Fs_kHz
	}
	if psDec.LossCnt == 0 && psDec.PrevSignalType == TYPE_NO_VOICE_ACTIVITY {
		for i = 0; i < psDec.LPC_order; i++ {
			psCNG.CNG_smth_NLSF_Q15[i] += int16(int32(((int(int32(psDec.PrevNLSF_Q15[i])) - int(int32(psCNG.CNG_smth_NLSF_Q15[i]))) * CNG_NLSF_SMTH_Q16) >> 16))
		}
		max_Gain_Q16 = 0
		subfr = 0
		for i = 0; i < psDec.Nb_subfr; i++ {
			if int(psDecCtrl.Gains_Q16[i]) > int(max_Gain_Q16) {
				max_Gain_Q16 = psDecCtrl.Gains_Q16[i]
				subfr = i
			}
		}
		libc.MemMove(unsafe.Pointer(&psCNG.CNG_exc_buf_Q14[psDec.Subfr_length]), unsafe.Pointer(&psCNG.CNG_exc_buf_Q14[0]), (psDec.Nb_subfr-1)*psDec.Subfr_length*int(unsafe.Sizeof(int32(0))))
		libc.MemCpy(unsafe.Pointer(&psCNG.CNG_exc_buf_Q14[0]), unsafe.Pointer(&psDec.Exc_Q14[subfr*psDec.Subfr_length]), psDec.Subfr_length*int(unsafe.Sizeof(int32(0))))
		for i = 0; i < psDec.Nb_subfr; i++ {
			psCNG.CNG_smth_Gain_Q16 += int32(((int(psDecCtrl.Gains_Q16[i]) - int(psCNG.CNG_smth_Gain_Q16)) * CNG_GAIN_SMTH_Q16) >> 16)
			if int(int32((int64(psCNG.CNG_smth_Gain_Q16)*CNG_GAIN_SMTH_THRESHOLD_Q16)>>16)) > int(psDecCtrl.Gains_Q16[i]) {
				psCNG.CNG_smth_Gain_Q16 = psDecCtrl.Gains_Q16[i]
			}
		}
	}
	if psDec.LossCnt != 0 {
		CNG_sig_Q14 := make([]int32, length+MAX_LPC_ORDER)
		gain_Q16 = int32((int64(psDec.SPLC.RandScale_Q14) * int64(psDec.SPLC.PrevGain_Q16[1])) >> 16)
		if int(gain_Q16) >= (1<<21) || int(psCNG.CNG_smth_Gain_Q16) > (1<<23) {
			gain_Q16 = int32((int(gain_Q16) >> 16) * (int(gain_Q16) >> 16))
			gain_Q16 = int32(((int(psCNG.CNG_smth_Gain_Q16) >> 16) * (int(psCNG.CNG_smth_Gain_Q16) >> 16)) - int(int32(int(uint32(gain_Q16))<<5)))
			gain_Q16 = int32(int(uint32(silk_SQRT_APPROX(gain_Q16))) << 16)
		} else {
			gain_Q16 = int32((int64(gain_Q16) * int64(gain_Q16)) >> 16)
			gain_Q16 = int32(int(int32((int64(psCNG.CNG_smth_Gain_Q16)*int64(psCNG.CNG_smth_Gain_Q16))>>16)) - int(int32(int(uint32(gain_Q16))<<5)))
			gain_Q16 = int32(int(uint32(silk_SQRT_APPROX(gain_Q16))) << 8)
		}
		gain_Q10 = int32(int(gain_Q16) >> 6)
		silk_CNG_exc(CNG_sig_Q14[MAX_LPC_ORDER:], psCNG.CNG_exc_buf_Q14[:], length, &psCNG.Rand_seed)
		NLSF2A(A_Q12[:], psCNG.CNG_smth_NLSF_Q15[:], psDec.LPC_order, psDec.Arch)
		libc.MemCpy(unsafe.Pointer(&CNG_sig_Q14[0]), unsafe.Pointer(&psCNG.CNG_synth_state[0]), int(MAX_LPC_ORDER*unsafe.Sizeof(int32(0))))
		for i = 0; i < length; i++ {
			LPC_pred_Q10 = int32(psDec.LPC_order >> 1)
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-1]) * int64(A_Q12[0])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-2]) * int64(A_Q12[1])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-3]) * int64(A_Q12[2])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-4]) * int64(A_Q12[3])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-5]) * int64(A_Q12[4])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-6]) * int64(A_Q12[5])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-7]) * int64(A_Q12[6])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-8]) * int64(A_Q12[7])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-9]) * int64(A_Q12[8])) >> 16))
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-10]) * int64(A_Q12[9])) >> 16))
			if psDec.LPC_order == 16 {
				LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-11]) * int64(A_Q12[10])) >> 16))
				LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-12]) * int64(A_Q12[11])) >> 16))
				LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-13]) * int64(A_Q12[12])) >> 16))
				LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-14]) * int64(A_Q12[13])) >> 16))
				LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-15]) * int64(A_Q12[14])) >> 16))
				LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(CNG_sig_Q14[MAX_LPC_ORDER+i-16]) * int64(A_Q12[15])) >> 16))
			}
			if ((int(uint32(CNG_sig_Q14[MAX_LPC_ORDER+i])) + int(uint32(int32(int(uint32(int32(func() int {
				if (int(math.MinInt32) >> 4) > (int(math.MaxInt32 >> 4)) {
					if int(LPC_pred_Q10) > (int(math.MinInt32) >> 4) {
						return int(math.MinInt32) >> 4
					}
					if int(LPC_pred_Q10) < (int(math.MaxInt32 >> 4)) {
						return int(math.MaxInt32 >> 4)
					}
					return int(LPC_pred_Q10)
				}
				if int(LPC_pred_Q10) > (int(math.MaxInt32 >> 4)) {
					return int(math.MaxInt32 >> 4)
				}
				if int(LPC_pred_Q10) < (int(math.MinInt32) >> 4) {
					return int(math.MinInt32) >> 4
				}
				return int(LPC_pred_Q10)
			}())))<<4)))) & 0x80000000) == 0 {
				if ((int(CNG_sig_Q14[MAX_LPC_ORDER+i]) & int(int32(int(uint32(int32(func() int {
					if (int(math.MinInt32) >> 4) > (int(math.MaxInt32 >> 4)) {
						if int(LPC_pred_Q10) > (int(math.MinInt32) >> 4) {
							return int(math.MinInt32) >> 4
						}
						if int(LPC_pred_Q10) < (int(math.MaxInt32 >> 4)) {
							return int(math.MaxInt32 >> 4)
						}
						return int(LPC_pred_Q10)
					}
					if int(LPC_pred_Q10) > (int(math.MaxInt32 >> 4)) {
						return int(math.MaxInt32 >> 4)
					}
					if int(LPC_pred_Q10) < (int(math.MinInt32) >> 4) {
						return int(math.MinInt32) >> 4
					}
					return int(LPC_pred_Q10)
				}())))<<4))) & 0x80000000) != 0 {
					CNG_sig_Q14[MAX_LPC_ORDER+i] = math.MinInt32
				} else {
					CNG_sig_Q14[MAX_LPC_ORDER+i] = int32(int(CNG_sig_Q14[MAX_LPC_ORDER+i]) + int(int32(int(uint32(int32(func() int {
						if (int(math.MinInt32) >> 4) > (int(math.MaxInt32 >> 4)) {
							if int(LPC_pred_Q10) > (int(math.MinInt32) >> 4) {
								return int(math.MinInt32) >> 4
							}
							if int(LPC_pred_Q10) < (int(math.MaxInt32 >> 4)) {
								return int(math.MaxInt32 >> 4)
							}
							return int(LPC_pred_Q10)
						}
						if int(LPC_pred_Q10) > (int(math.MaxInt32 >> 4)) {
							return int(math.MaxInt32 >> 4)
						}
						if int(LPC_pred_Q10) < (int(math.MinInt32) >> 4) {
							return int(math.MinInt32) >> 4
						}
						return int(LPC_pred_Q10)
					}())))<<4)))
				}
			} else if ((int(CNG_sig_Q14[MAX_LPC_ORDER+i]) | int(int32(int(uint32(int32(func() int {
				if (int(math.MinInt32) >> 4) > (int(math.MaxInt32 >> 4)) {
					if int(LPC_pred_Q10) > (int(math.MinInt32) >> 4) {
						return int(math.MinInt32) >> 4
					}
					if int(LPC_pred_Q10) < (int(math.MaxInt32 >> 4)) {
						return int(math.MaxInt32 >> 4)
					}
					return int(LPC_pred_Q10)
				}
				if int(LPC_pred_Q10) > (int(math.MaxInt32 >> 4)) {
					return int(math.MaxInt32 >> 4)
				}
				if int(LPC_pred_Q10) < (int(math.MinInt32) >> 4) {
					return int(math.MinInt32) >> 4
				}
				return int(LPC_pred_Q10)
			}())))<<4))) & 0x80000000) == 0 {
				CNG_sig_Q14[MAX_LPC_ORDER+i] = math.MaxInt32
			} else {
				CNG_sig_Q14[MAX_LPC_ORDER+i] = int32(int(CNG_sig_Q14[MAX_LPC_ORDER+i]) + int(int32(int(uint32(int32(func() int {
					if (int(math.MinInt32) >> 4) > (int(math.MaxInt32 >> 4)) {
						if int(LPC_pred_Q10) > (int(math.MinInt32) >> 4) {
							return int(math.MinInt32) >> 4
						}
						if int(LPC_pred_Q10) < (int(math.MaxInt32 >> 4)) {
							return int(math.MaxInt32 >> 4)
						}
						return int(LPC_pred_Q10)
					}
					if int(LPC_pred_Q10) > (int(math.MaxInt32 >> 4)) {
						return int(math.MaxInt32 >> 4)
					}
					if int(LPC_pred_Q10) < (int(math.MinInt32) >> 4) {
						return int(math.MinInt32) >> 4
					}
					return int(LPC_pred_Q10)
				}())))<<4)))
			}
			if (int(int32(frame[i])) + (func() int {
				if (func() int {
					if 8 == 1 {
						return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
					}
					return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
				}()) > math.MaxInt16 {
					return math.MaxInt16
				}
				if (func() int {
					if 8 == 1 {
						return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
					}
					return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
				}()) < int(math.MinInt16) {
					return math.MinInt16
				}
				if 8 == 1 {
					return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
				}
				return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
			}())) > math.MaxInt16 {
				frame[i] = math.MaxInt16
			} else if (int(int32(frame[i])) + (func() int {
				if (func() int {
					if 8 == 1 {
						return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
					}
					return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
				}()) > math.MaxInt16 {
					return math.MaxInt16
				}
				if (func() int {
					if 8 == 1 {
						return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
					}
					return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
				}()) < int(math.MinInt16) {
					return math.MinInt16
				}
				if 8 == 1 {
					return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
				}
				return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
			}())) < int(math.MinInt16) {
				frame[i] = math.MinInt16
			} else {
				frame[i] = int16(int(int32(frame[i])) + (func() int {
					if (func() int {
						if 8 == 1 {
							return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
						}
						return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
					}()) > math.MaxInt16 {
						return math.MaxInt16
					}
					if (func() int {
						if 8 == 1 {
							return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
						}
						return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
					}()) < int(math.MinInt16) {
						return math.MinInt16
					}
					if 8 == 1 {
						return (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> 1) + (int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) & 1)
					}
					return ((int(int32((int64(CNG_sig_Q14[MAX_LPC_ORDER+i])*int64(gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
				}()))
			}
		}
		libc.MemCpy(unsafe.Pointer(&psCNG.CNG_synth_state[0]), unsafe.Pointer(&CNG_sig_Q14[length]), int(MAX_LPC_ORDER*unsafe.Sizeof(int32(0))))
	} else {
		libc.MemSet(unsafe.Pointer(&psCNG.CNG_synth_state[0]), 0, psDec.LPC_order*int(unsafe.Sizeof(int32(0))))
	}
}
