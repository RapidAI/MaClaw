package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

const BWE_COEF = 0.99
const V_PITCH_GAIN_START_MIN_Q14 = 11469
const NB_ATT = 2
const V_PITCH_GAIN_START_MAX_Q14 = 15565
const MAX_PITCH_LAG_MS = 18
const RAND_BUF_SIZE = 128
const RAND_BUF_MASK uint8 = math.MaxInt8
const LOG2_INV_LPC_GAIN_HIGH_THRES = 3
const LOG2_INV_LPC_GAIN_LOW_THRES = 8
const PITCH_DRIFT_FAC_Q16 = 655

var HARM_ATT_Q15 [2]int16 = [2]int16{32440, 31130}
var PLC_RAND_ATTENUATE_V_Q15 [2]int16 = [2]int16{31130, 26214}
var PLC_RAND_ATTENUATE_UV_Q15 [2]int16 = [2]int16{32440, 29491}

func PLC_Reset(psDec *DecoderState) {
	psDec.SPLC.PitchL_Q8 = int32(int(uint32(int32(psDec.Frame_length))) << (8 - 1))
	psDec.SPLC.PrevGain_Q16[0] = int32(math.Floor(1*(1<<16) + 0.5))
	psDec.SPLC.PrevGain_Q16[1] = int32(math.Floor(1*(1<<16) + 0.5))
	psDec.SPLC.Subfr_length = 20
	psDec.SPLC.Nb_subfr = 2
}
func PLC(psDec *DecoderState, psDecCtrl *DecoderControl, frame []int16, lost int, arch int) {
	if psDec.Fs_kHz != psDec.SPLC.Fs_kHz {
		PLC_Reset(psDec)
		psDec.SPLC.Fs_kHz = psDec.Fs_kHz
	}
	if lost != 0 {
		silk_PLC_conceal(psDec, psDecCtrl, frame, arch)
		psDec.LossCnt++
	} else {
		silk_PLC_update(psDec, psDecCtrl)
	}
}
func silk_PLC_update(psDec *DecoderState, psDecCtrl *DecoderControl) {
	var (
		LTP_Gain_Q14      int32
		temp_LTP_Gain_Q14 int32
	)
	psPLC := &psDec.SPLC
	psDec.PrevSignalType = int(psDec.Indices.SignalType)
	LTP_Gain_Q14 = 0
	if int(psDec.Indices.SignalType) == TYPE_VOICED {
		for j := 0; j*psDec.Subfr_length < psDecCtrl.PitchL[psDec.Nb_subfr-1]; j++ {
			if j == psDec.Nb_subfr {
				break
			}
			temp_LTP_Gain_Q14 = 0
			for i := 0; i < LTP_ORDER; i++ {
				temp_LTP_Gain_Q14 += int32(psDecCtrl.LTPCoef_Q14[(psDec.Nb_subfr-1-j)*LTP_ORDER+i])
			}
			if int(temp_LTP_Gain_Q14) > int(LTP_Gain_Q14) {
				LTP_Gain_Q14 = temp_LTP_Gain_Q14
				libc.MemCpy(unsafe.Pointer(&psPLC.LTPCoef_Q14[0]), unsafe.Pointer(&psDecCtrl.LTPCoef_Q14[int(int32(int16(psDec.Nb_subfr-1-j)))*LTP_ORDER]), int(LTP_ORDER*unsafe.Sizeof(int16(0))))
				psPLC.PitchL_Q8 = int32(int(uint32(int32(psDecCtrl.PitchL[psDec.Nb_subfr-1-j]))) << 8)
			}
		}
		libc.MemSet(unsafe.Pointer(&psPLC.LTPCoef_Q14[0]), 0, int(LTP_ORDER*unsafe.Sizeof(int16(0))))
		psPLC.LTPCoef_Q14[int(LTP_ORDER/2)] = int16(LTP_Gain_Q14)
		if int(LTP_Gain_Q14) < V_PITCH_GAIN_START_MIN_Q14 {
			var (
				scale_Q10 int
				tmp       int32
			)
			tmp = int32(int(V_PITCH_GAIN_START_MIN_Q14 << 10))
			scale_Q10 = int(int32(int(tmp) / (func() int {
				if int(LTP_Gain_Q14) > 1 {
					return int(LTP_Gain_Q14)
				}
				return 1
			}())))
			for i := 0; i < LTP_ORDER; i++ {
				psPLC.LTPCoef_Q14[i] = int16((int(int32(psPLC.LTPCoef_Q14[i])) * int(int32(int16(scale_Q10)))) >> 10)
			}
		} else if int(LTP_Gain_Q14) > V_PITCH_GAIN_START_MAX_Q14 {
			var (
				scale_Q14 int
				tmp       int32
			)
			tmp = int32(int(V_PITCH_GAIN_START_MAX_Q14 << 14))
			scale_Q14 = int(int32(int(tmp) / (func() int {
				if int(LTP_Gain_Q14) > 1 {
					return int(LTP_Gain_Q14)
				}
				return 1
			}())))
			for i := 0; i < LTP_ORDER; i++ {
				psPLC.LTPCoef_Q14[i] = int16((int(int32(psPLC.LTPCoef_Q14[i])) * int(int32(int16(scale_Q14)))) >> 14)
			}
		}
	} else {
		psPLC.PitchL_Q8 = int32(int(uint32(int32(int(int32(int16(psDec.Fs_kHz)))*18))) << 8)
		libc.MemSet(unsafe.Pointer(&psPLC.LTPCoef_Q14[0]), 0, int(LTP_ORDER*unsafe.Sizeof(int16(0))))
	}
	libc.MemCpy(unsafe.Pointer(&psPLC.PrevLPC_Q12[0]), unsafe.Pointer(&(psDecCtrl.PredCoef_Q12[1])[0]), psDec.LPC_order*int(unsafe.Sizeof(int16(0))))
	psPLC.PrevLTP_scale_Q14 = int16(psDecCtrl.LTP_scale_Q14)
	libc.MemCpy(unsafe.Pointer(&psPLC.PrevGain_Q16[0]), unsafe.Pointer(&psDecCtrl.Gains_Q16[psDec.Nb_subfr-2]), int(2*unsafe.Sizeof(int32(0))))
	psPLC.Subfr_length = psDec.Subfr_length
	psPLC.Nb_subfr = psDec.Nb_subfr
}
func silk_PLC_energy(energy1 *int32, shift1 *int, energy2 *int32, shift2 *int, exc_Q14 []int32, prevGain_Q10 []int32, subfr_length int, nb_subfr int) {
	exc_buf := make([]int16, subfr_length*2)
	exc_buf_ptr := exc_buf
	for k := 0; k < 2; k++ {
		for i := 0; i < subfr_length; i++ {
			if (int(int32((int64(exc_Q14[i+(k+nb_subfr-2)*subfr_length])*int64(prevGain_Q10[k]))>>16)) >> 8) > math.MaxInt16 {
				exc_buf_ptr[i] = math.MaxInt16
			} else if (int(int32((int64(exc_Q14[i+(k+nb_subfr-2)*subfr_length])*int64(prevGain_Q10[k]))>>16)) >> 8) < int(math.MinInt16) {
				exc_buf_ptr[i] = math.MinInt16
			} else {
				exc_buf_ptr[i] = int16(int(int32((int64(exc_Q14[i+(k+nb_subfr-2)*subfr_length])*int64(prevGain_Q10[k]))>>16)) >> 8)
			}
		}
		exc_buf_ptr = exc_buf_ptr[subfr_length:]
	}
	silk_sum_sqr_shift(energy1, shift1, exc_buf, subfr_length)
	silk_sum_sqr_shift(energy2, shift2, exc_buf[subfr_length:], subfr_length)
}
func silk_PLC_conceal(psDec *DecoderState, psDecCtrl *DecoderControl, frame []int16, arch int) {
	var (
		lag            int
		idx            int
		sLTP_buf_idx   int
		shift1         int
		shift2         int
		rand_seed      int32
		harm_Gain_Q15  int32
		rand_Gain_Q15  int32
		inv_gain_Q30   int32
		energy1        int32
		energy2        int32
		LPC_pred_Q10   int32
		LTP_pred_Q12   int32
		rand_scale_Q14 int16
		A_Q12          [16]int16
		prevGain_Q10   [2]int32
	)
	psPLC := &psDec.SPLC
	sLTP_Q14 := make([]int32, psDec.Ltp_mem_length+psDec.Frame_length)
	sLTP := make([]int16, psDec.Ltp_mem_length)
	prevGain_Q10[0] = int32(int(psPLC.PrevGain_Q16[0]) >> 6)
	prevGain_Q10[1] = int32(int(psPLC.PrevGain_Q16[1]) >> 6)
	if psDec.First_frame_after_reset != 0 {
		*(*[16]int16)(unsafe.Pointer(&psPLC.PrevLPC_Q12[0])) = [16]int16{}
	}
	silk_PLC_energy(&energy1, &shift1, &energy2, &shift2, psDec.Exc_Q14[:], prevGain_Q10[:], psDec.Subfr_length, psDec.Nb_subfr)
	var rand_ptr []int32
	if (int(energy1) >> shift2) < (int(energy2) >> shift1) {
		rand_ptr = psDec.Exc_Q14[silk_max_int(0, (psPLC.Nb_subfr-1)*psPLC.Subfr_length-RAND_BUF_SIZE):]
	} else {
		rand_ptr = psDec.Exc_Q14[silk_max_int(0, psPLC.Nb_subfr*psPLC.Subfr_length-RAND_BUF_SIZE):]
	}
	B_Q14 := psPLC.LTPCoef_Q14[:]
	rand_scale_Q14 = psPLC.RandScale_Q14
	harm_Gain_Q15 = int32(HARM_ATT_Q15[silk_min_int(int(NB_ATT-1), psDec.LossCnt)])
	if psDec.PrevSignalType == TYPE_VOICED {
		rand_Gain_Q15 = int32(PLC_RAND_ATTENUATE_V_Q15[silk_min_int(int(NB_ATT-1), psDec.LossCnt)])
	} else {
		rand_Gain_Q15 = int32(PLC_RAND_ATTENUATE_UV_Q15[silk_min_int(int(NB_ATT-1), psDec.LossCnt)])
	}
	BwExpander(psPLC.PrevLPC_Q12[:], psDec.LPC_order, int32(math.Floor(BWE_COEF*(1<<16)+0.5)))
	libc.MemCpy(unsafe.Pointer(&A_Q12[0]), unsafe.Pointer(&psPLC.PrevLPC_Q12[0]), psDec.LPC_order*int(unsafe.Sizeof(int16(0))))
	if psDec.LossCnt == 0 {
		rand_scale_Q14 = 1 << 14
		if psDec.PrevSignalType == TYPE_VOICED {
			for i := 0; i < LTP_ORDER; i++ {
				rand_scale_Q14 -= B_Q14[i]
			}
			rand_scale_Q14 = silk_max_16(3277, rand_scale_Q14)
			rand_scale_Q14 = int16((int(int32(rand_scale_Q14)) * int(int32(psPLC.PrevLTP_scale_Q14))) >> 14)
		} else {
			var (
				invGain_Q30    int32
				down_scale_Q30 int32
			)
			invGain_Q30 = func() int32 {
				_ = arch
				return silk_LPC_inverse_pred_gain_c(psPLC.PrevLPC_Q12[:], psDec.LPC_order)
			}()
			down_scale_Q30 = silk_min_32(int32(int((1<<30)>>LOG2_INV_LPC_GAIN_HIGH_THRES)), invGain_Q30)
			down_scale_Q30 = silk_max_32(int32(int((1<<30)>>LOG2_INV_LPC_GAIN_LOW_THRES)), down_scale_Q30)
			down_scale_Q30 = int32(int(uint32(down_scale_Q30)) << LOG2_INV_LPC_GAIN_HIGH_THRES)
			rand_Gain_Q15 = int32(int(int32((int64(down_scale_Q30)*int64(int16(rand_Gain_Q15)))>>16)) >> 14)
		}
	}
	rand_seed = psPLC.Rand_seed
	if 8 == 1 {
		lag = (int(psPLC.PitchL_Q8) >> 1) + (int(psPLC.PitchL_Q8) & 1)
	} else {
		lag = ((int(psPLC.PitchL_Q8) >> (8 - 1)) + 1) >> 1
	}
	sLTP_buf_idx = psDec.Ltp_mem_length
	idx = psDec.Ltp_mem_length - lag - psDec.LPC_order - int(LTP_ORDER/2)
	silk_LPC_analysis_filter(sLTP[idx:], psDec.OutBuf[idx:], A_Q12[:], int32(psDec.Ltp_mem_length-idx), int32(psDec.LPC_order), arch)
	inv_gain_Q30 = silk_INVERSE32_varQ(psPLC.PrevGain_Q16[1], 46)
	if int(inv_gain_Q30) < (int(math.MaxInt32 >> 1)) {
		inv_gain_Q30 = inv_gain_Q30
	} else {
		inv_gain_Q30 = int32(int(math.MaxInt32 >> 1))
	}
	for i := idx + psDec.LPC_order; i < psDec.Ltp_mem_length; i++ {
		sLTP_Q14[i] = int32((int64(inv_gain_Q30) * int64(sLTP[i])) >> 16)
	}
	for k := 0; k < psDec.Nb_subfr; k++ {
		pred_lag_i := sLTP_buf_idx - lag + LTP_ORDER/2
		for i := 0; i < psDec.Subfr_length; i++ {
			LTP_pred_Q12 = 2
			LTP_pred_Q12 = int32(int64(LTP_pred_Q12) + ((int64(sLTP_Q14[pred_lag_i+0]) * int64(B_Q14[0])) >> 16))
			LTP_pred_Q12 = int32(int64(LTP_pred_Q12) + ((int64(sLTP_Q14[pred_lag_i-1]) * int64(B_Q14[1])) >> 16))
			LTP_pred_Q12 = int32(int64(LTP_pred_Q12) + ((int64(sLTP_Q14[pred_lag_i-2]) * int64(B_Q14[2])) >> 16))
			LTP_pred_Q12 = int32(int64(LTP_pred_Q12) + ((int64(sLTP_Q14[pred_lag_i-3]) * int64(B_Q14[3])) >> 16))
			LTP_pred_Q12 = int32(int64(LTP_pred_Q12) + ((int64(sLTP_Q14[pred_lag_i-4]) * int64(B_Q14[4])) >> 16))
			pred_lag_i++
			rand_seed = int32(RAND_INCREMENT + int(uint32(int32(int(uint32(rand_seed))*RAND_MULTIPLIER))))
			idx = (int(rand_seed) >> 25) & (int(RAND_BUF_SIZE - 1))
			sLTP_Q14[sLTP_buf_idx] = int32(int(uint32(int32(int64(LTP_pred_Q12)+((int64(rand_ptr[idx])*int64(rand_scale_Q14))>>16)))) << 2)
			sLTP_buf_idx++
		}
		for j := 0; j < LTP_ORDER; j++ {
			B_Q14[j] = int16((int(int32(int16(harm_Gain_Q15))) * int(int32(B_Q14[j]))) >> 15)
		}
		rand_scale_Q14 = int16((int(int32(rand_scale_Q14)) * int(int32(int16(rand_Gain_Q15)))) >> 15)
		psPLC.PitchL_Q8 = int32(int(psPLC.PitchL_Q8) + ((int(psPLC.PitchL_Q8) * PITCH_DRIFT_FAC_Q16) >> 16))
		psPLC.PitchL_Q8 = silk_min_32(psPLC.PitchL_Q8, int32(int(uint32(int32(MAX_PITCH_LAG_MS*int(int32(int16(psDec.Fs_kHz))))))<<8))
		if 8 == 1 {
			lag = (int(psPLC.PitchL_Q8) >> 1) + (int(psPLC.PitchL_Q8) & 1)
		} else {
			lag = ((int(psPLC.PitchL_Q8) >> (8 - 1)) + 1) >> 1
		}
	}
	sLPC_Q14_ptr := sLTP_Q14[psDec.Ltp_mem_length-MAX_LPC_ORDER:]
	libc.MemCpy(unsafe.Pointer(&sLPC_Q14_ptr[0]), unsafe.Pointer(&psDec.SLPC_Q14_buf[0]), int(MAX_LPC_ORDER*unsafe.Sizeof(int32(0))))
	for i := 0; i < psDec.Frame_length; i++ {
		LPC_pred_Q10 = int32(psDec.LPC_order >> 1)
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-1]) * int64(A_Q12[0])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-2]) * int64(A_Q12[1])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-3]) * int64(A_Q12[2])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-4]) * int64(A_Q12[3])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-5]) * int64(A_Q12[4])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-6]) * int64(A_Q12[5])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-7]) * int64(A_Q12[6])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-8]) * int64(A_Q12[7])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-9]) * int64(A_Q12[8])) >> 16))
		LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-10]) * int64(A_Q12[9])) >> 16))
		for j := 10; j < psDec.LPC_order; j++ {
			LPC_pred_Q10 = int32(int64(LPC_pred_Q10) + ((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i-j-1]) * int64(A_Q12[j])) >> 16))
		}
		if ((int(uint32(sLPC_Q14_ptr[MAX_LPC_ORDER+i])) + int(uint32(int32(int(uint32(int32(func() int {
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
			if ((int(sLPC_Q14_ptr[MAX_LPC_ORDER+i]) & int(int32(int(uint32(int32(func() int {
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
				sLPC_Q14_ptr[MAX_LPC_ORDER+i] = math.MinInt32
			} else {
				sLPC_Q14_ptr[MAX_LPC_ORDER+i] = int32(int(sLPC_Q14_ptr[MAX_LPC_ORDER+i]) + int(int32(int(uint32(int32(func() int {
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
		} else if ((int(sLPC_Q14_ptr[MAX_LPC_ORDER+i]) | int(int32(int(uint32(int32(func() int {
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
			sLPC_Q14_ptr[MAX_LPC_ORDER+i] = math.MaxInt32
		} else {
			sLPC_Q14_ptr[MAX_LPC_ORDER+i] = int32(int(sLPC_Q14_ptr[MAX_LPC_ORDER+i]) + int(int32(int(uint32(int32(func() int {
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
		if (func() int {
			if (func() int {
				if 8 == 1 {
					return (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1)
				}
				return ((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1
			}()) > math.MaxInt16 {
				return math.MaxInt16
			}
			if (func() int {
				if 8 == 1 {
					return (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1)
				}
				return ((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1
			}()) < int(math.MinInt16) {
				return math.MinInt16
			}
			if 8 == 1 {
				return (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1)
			}
			return ((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			frame[i] = math.MaxInt16
		} else if (func() int {
			if (func() int {
				if 8 == 1 {
					return (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1)
				}
				return ((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1
			}()) > math.MaxInt16 {
				return math.MaxInt16
			}
			if (func() int {
				if 8 == 1 {
					return (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1)
				}
				return ((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1
			}()) < int(math.MinInt16) {
				return math.MinInt16
			}
			if 8 == 1 {
				return (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1)
			}
			return ((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			frame[i] = math.MinInt16
		} else if (func() int {
			if 8 == 1 {
				return (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1)
			}
			return ((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			frame[i] = math.MaxInt16
		} else if (func() int {
			if 8 == 1 {
				return (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1)
			}
			return ((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			frame[i] = math.MinInt16
		} else if 8 == 1 {
			frame[i] = int16((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> 1) + (int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) & 1))
		} else {
			frame[i] = int16(((int(int32((int64(sLPC_Q14_ptr[MAX_LPC_ORDER+i])*int64(prevGain_Q10[1]))>>16)) >> (8 - 1)) + 1) >> 1)
		}
	}
	libc.MemCpy(unsafe.Pointer(&psDec.SLPC_Q14_buf[0]), unsafe.Pointer(&sLPC_Q14_ptr[psDec.Frame_length]), int(MAX_LPC_ORDER*unsafe.Sizeof(int32(0))))
	psPLC.Rand_seed = rand_seed
	psPLC.RandScale_Q14 = rand_scale_Q14
	for i := 0; i < MAX_NB_SUBFR; i++ {
		psDecCtrl.PitchL[i] = lag
	}
}
func PLC_glue_frames(psDec *DecoderState, frame []int16, length int) {
	var (
		i            int
		energy_shift int
		energy       int32
	)
	psPLC := &psDec.SPLC
	if psDec.LossCnt != 0 {
		silk_sum_sqr_shift(&psPLC.Conc_energy, &psPLC.Conc_energy_shift, frame, length)
		psPLC.Last_frame_lost = 1
	} else {
		if psDec.SPLC.Last_frame_lost != 0 {
			silk_sum_sqr_shift(&energy, &energy_shift, frame, length)
			if energy_shift > psPLC.Conc_energy_shift {
				psPLC.Conc_energy = int32(int(psPLC.Conc_energy) >> (energy_shift - psPLC.Conc_energy_shift))
			} else if energy_shift < psPLC.Conc_energy_shift {
				energy = int32(int(energy) >> (psPLC.Conc_energy_shift - energy_shift))
			}
			if int(energy) > int(psPLC.Conc_energy) {
				var (
					frac_Q24  int32
					LZ        int32
					gain_Q16  int32
					slope_Q16 int32
				)
				LZ = silk_CLZ32(psPLC.Conc_energy)
				LZ = int32(int(LZ) - 1)
				psPLC.Conc_energy = int32(int(uint32(psPLC.Conc_energy)) << int(LZ))
				energy = int32(int(energy) >> int(silk_max_32(int32(24-int(LZ)), 0)))
				frac_Q24 = int32(int(psPLC.Conc_energy) / (func() int {
					if int(energy) > 1 {
						return int(energy)
					}
					return 1
				}()))
				gain_Q16 = int32(int(uint32(silk_SQRT_APPROX(frac_Q24))) << 4)
				slope_Q16 = int32(((1 << 16) - int(gain_Q16)) / length)
				slope_Q16 = int32(int(uint32(slope_Q16)) << 2)
				for i = 0; i < length; i++ {
					frame[i] = int16(int32((int64(gain_Q16) * int64(frame[i])) >> 16))
					gain_Q16 += slope_Q16
					if int(gain_Q16) > 1<<16 {
						break
					}
				}
			}
		}
		psPLC.Last_frame_lost = 0
	}
}
