package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

func silk_noise_shape_quantizer_short_prediction_c(buf32 []int32, i int, coef16 []int16, order int) int32 {
	out := int32(order >> 1)
	out = int32(int64(out) + ((int64(buf32[i+0]) * int64(coef16[0])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-1]) * int64(coef16[1])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-2]) * int64(coef16[2])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-3]) * int64(coef16[3])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-4]) * int64(coef16[4])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-5]) * int64(coef16[5])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-6]) * int64(coef16[6])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-7]) * int64(coef16[7])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-8]) * int64(coef16[8])) >> 16))
	out = int32(int64(out) + ((int64(buf32[i-9]) * int64(coef16[9])) >> 16))
	if order == 16 {
		out = int32(int64(out) + ((int64(buf32[i-10]) * int64(coef16[10])) >> 16))
		out = int32(int64(out) + ((int64(buf32[i-11]) * int64(coef16[11])) >> 16))
		out = int32(int64(out) + ((int64(buf32[i-12]) * int64(coef16[12])) >> 16))
		out = int32(int64(out) + ((int64(buf32[i-13]) * int64(coef16[13])) >> 16))
		out = int32(int64(out) + ((int64(buf32[i-14]) * int64(coef16[14])) >> 16))
		out = int32(int64(out) + ((int64(buf32[i-15]) * int64(coef16[15])) >> 16))
	}
	return out
}
func silk_NSQ_noise_shape_feedback_loop_c(data0 *int32, data1 []int32, coef []int16, order int) int32 {
	var (
		out  int32
		tmp1 int32
		tmp2 int32
		j    int
	)
	tmp2 = *data0
	tmp1 = data1[0]
	data1[0] = tmp2
	out = int32(order >> 1)
	out = int32(int64(out) + ((int64(tmp2) * int64(coef[0])) >> 16))
	for j = 2; j < order; j += 2 {
		tmp2 = data1[j-1]
		data1[j-1] = tmp1
		out = int32(int64(out) + ((int64(tmp1) * int64(coef[j-1])) >> 16))
		tmp1 = data1[j+0]
		data1[j+0] = tmp2
		out = int32(int64(out) + ((int64(tmp2) * int64(coef[j])) >> 16))
	}
	data1[order-1] = tmp1
	out = int32(int64(out) + ((int64(tmp1) * int64(coef[order-1])) >> 16))
	out = int32(int(uint32(out)) << 1)
	return out
}
func NSQ_c(psEncC *EncoderState, NSQ *NSQState, psIndices *SideInfoIndices, x16 []int16, pulses []int8, PredCoef_Q12 [32]int16, LTPCoef_Q14 [20]int16, AR_Q13 [96]int16, HarmShapeGain_Q14 [4]int, Tilt_Q14 [4]int, LF_shp_Q14 [4]int32, Gains_Q16 [4]int32, pitchL [4]int, Lambda_Q10 int, LTP_scale_Q14 int) {
	var (
		lag                    int
		start_idx              int
		LSF_interpolation_flag int
		HarmShapeFIRPacked_Q14 int32
		offset_Q10             int
	)
	NSQ.Rand_seed = int32(psIndices.Seed)
	lag = NSQ.LagPrev
	offset_Q10 = int(silk_Quantization_Offsets_Q10[int(psIndices.SignalType)>>1][psIndices.QuantOffsetType])
	if int(psIndices.NLSFInterpCoef_Q2) == 4 {
		LSF_interpolation_flag = 0
	} else {
		LSF_interpolation_flag = 1
	}
	sLTP_Q15 := make([]int32, psEncC.Ltp_mem_length+psEncC.Frame_length)
	sLTP := make([]int16, psEncC.Ltp_mem_length+psEncC.Frame_length)
	x_sc_Q10 := make([]int32, psEncC.Subfr_length)
	NSQ.SLTP_shp_buf_idx = psEncC.Ltp_mem_length
	NSQ.SLTP_buf_idx = psEncC.Ltp_mem_length
	pxq := NSQ.Xq[psEncC.Ltp_mem_length:]
	for k := 0; k < psEncC.Nb_subfr; k++ {
		A_Q12 := PredCoef_Q12[((k>>1)|(1-LSF_interpolation_flag))*MAX_LPC_ORDER:]
		B_Q14 := LTPCoef_Q14[k*LTP_ORDER:]
		AR_shp_Q13 := AR_Q13[k*MAX_SHAPE_LPC_ORDER:]
		HarmShapeFIRPacked_Q14 = int32((HarmShapeGain_Q14[k]) >> 2)
		HarmShapeFIRPacked_Q14 |= int32(int(uint32(int32((HarmShapeGain_Q14[k])>>1))) << 16)
		NSQ.Rewhite_flag = 0
		if int(psIndices.SignalType) == TYPE_VOICED {
			lag = pitchL[k]
			if (k & (3 - int(int32(int(uint32(int32(LSF_interpolation_flag)))<<1)))) == 0 {
				start_idx = psEncC.Ltp_mem_length - lag - psEncC.PredictLPCOrder - int(LTP_ORDER/2)
				silk_LPC_analysis_filter(sLTP[start_idx:], NSQ.Xq[start_idx+k*psEncC.Subfr_length:], []int16(A_Q12), int32(psEncC.Ltp_mem_length-start_idx), int32(psEncC.PredictLPCOrder), psEncC.Arch)
				NSQ.Rewhite_flag = 1
				NSQ.SLTP_buf_idx = psEncC.Ltp_mem_length
			}
		}
		silk_nsq_scale_states(psEncC, NSQ, x16, []int32(x_sc_Q10), []int16(sLTP), []int32(sLTP_Q15), k, LTP_scale_Q14, Gains_Q16, pitchL, int(psIndices.SignalType))
		silk_noise_shape_quantizer(NSQ, int(psIndices.SignalType), []int32(x_sc_Q10), pulses, []int16(pxq), []int32(sLTP_Q15), []int16(A_Q12), []int16(B_Q14), []int16(AR_shp_Q13), lag, HarmShapeFIRPacked_Q14, Tilt_Q14[k], LF_shp_Q14[k], Gains_Q16[k], Lambda_Q10, offset_Q10, psEncC.Subfr_length, psEncC.ShapingLPCOrder, psEncC.PredictLPCOrder, psEncC.Arch)
		x16 = x16[psEncC.Subfr_length:]
		pulses = pulses[psEncC.Subfr_length:]
		pxq = pxq[psEncC.Subfr_length:]
	}
	NSQ.LagPrev = pitchL[psEncC.Nb_subfr-1]
	libc.MemMove(unsafe.Pointer(&NSQ.Xq[0]), unsafe.Pointer(&NSQ.Xq[psEncC.Frame_length]), psEncC.Ltp_mem_length*int(unsafe.Sizeof(int16(0))))
	libc.MemMove(unsafe.Pointer(&NSQ.SLTP_shp_Q14[0]), unsafe.Pointer(&NSQ.SLTP_shp_Q14[psEncC.Frame_length]), psEncC.Ltp_mem_length*int(unsafe.Sizeof(int32(0))))
}
func silk_noise_shape_quantizer(NSQ *NSQState, signalType int, x_sc_Q10 []int32, pulses []int8, xq []int16, sLTP_Q15 []int32, a_Q12 []int16, b_Q14 []int16, AR_shp_Q13 []int16, lag int, HarmShapeFIRPacked_Q14 int32, Tilt_Q14 int, LF_shp_Q14 int32, Gain_Q16 int32, Lambda_Q10 int, offset_Q10 int, length int, shapingLPCOrder int, predictLPCOrder int, arch int) {
	var (
		i              int
		LTP_pred_Q13   int32
		LPC_pred_Q10   int32
		n_AR_Q12       int32
		n_LTP_Q13      int32
		n_LF_Q12       int32
		r_Q10          int32
		rr_Q10         int32
		q1_Q0          int32
		q1_Q10         int32
		q2_Q10         int32
		rd1_Q20        int32
		rd2_Q20        int32
		exc_Q14        int32
		LPC_exc_Q14    int32
		xq_Q14         int32
		Gain_Q10       int32
		tmp1           int32
		tmp2           int32
		sLF_AR_shp_Q14 int32
	)
	shp_lag_ind := NSQ.SLTP_shp_buf_idx - lag + int(HARM_SHAPE_FIR_TAPS/2)
	pred_lag_ind := NSQ.SLTP_buf_idx - lag + int(LTP_ORDER/2)
	Gain_Q10 = int32(int(Gain_Q16) >> 6)
	psLPC_Q14_ptr := NSQ.SLPC_Q14[MAX_LPC_ORDER-1:]
	psLPC_Q14_ind := 0
	for i = 0; i < length; i++ {
		NSQ.Rand_seed = int32(RAND_INCREMENT + int(uint32(int32(int(uint32(NSQ.Rand_seed))*RAND_MULTIPLIER))))
		LPC_pred_Q10 = func() int32 {
			_ = arch
			return silk_noise_shape_quantizer_short_prediction_c(NSQ.SLPC_Q14[:], MAX_LPC_ORDER-1+psLPC_Q14_ind, a_Q12, predictLPCOrder)
		}()
		if signalType == TYPE_VOICED {
			LTP_pred_Q13 = 2
			LTP_pred_Q13 = int32(int64(LTP_pred_Q13) + ((int64(sLTP_Q15[pred_lag_ind+0]) * int64(b_Q14[0])) >> 16))
			LTP_pred_Q13 = int32(int64(LTP_pred_Q13) + ((int64(sLTP_Q15[pred_lag_ind-1]) * int64(b_Q14[1])) >> 16))
			LTP_pred_Q13 = int32(int64(LTP_pred_Q13) + ((int64(sLTP_Q15[pred_lag_ind-2]) * int64(b_Q14[2])) >> 16))
			LTP_pred_Q13 = int32(int64(LTP_pred_Q13) + ((int64(sLTP_Q15[pred_lag_ind-3]) * int64(b_Q14[3])) >> 16))
			LTP_pred_Q13 = int32(int64(LTP_pred_Q13) + ((int64(sLTP_Q15[pred_lag_ind-4]) * int64(b_Q14[4])) >> 16))
			pred_lag_ind++
		} else {
			LTP_pred_Q13 = 0
		}
		n_AR_Q12 = func() int32 {
			_ = arch
			return silk_NSQ_noise_shape_feedback_loop_c(&NSQ.SDiff_shp_Q14, NSQ.SAR2_Q14[:], AR_shp_Q13, shapingLPCOrder)
		}()
		n_AR_Q12 = int32(int64(n_AR_Q12) + ((int64(NSQ.SLF_AR_shp_Q14) * int64(int16(Tilt_Q14))) >> 16))
		n_LF_Q12 = int32((int64(NSQ.SLTP_shp_Q14[NSQ.SLTP_shp_buf_idx-1]) * int64(int16(LF_shp_Q14))) >> 16)
		n_LF_Q12 = int32(int64(n_LF_Q12) + ((int64(NSQ.SLF_AR_shp_Q14) * (int64(LF_shp_Q14) >> 16)) >> 16))
		tmp1 = int32(int(int32(int(uint32(LPC_pred_Q10))<<2)) - int(n_AR_Q12))
		tmp1 = int32(int(tmp1) - int(n_LF_Q12))
		if lag > 0 {
			n_LTP_Q13 = int32(((func() int {
				if ((int(uint32(NSQ.SLTP_shp_Q14[shp_lag_ind+0])) + int(uint32(NSQ.SLTP_shp_Q14[shp_lag_ind-2]))) & 0x80000000) == 0 {
					if ((int(NSQ.SLTP_shp_Q14[shp_lag_ind+0]) & int(NSQ.SLTP_shp_Q14[shp_lag_ind-2])) & 0x80000000) != 0 {
						return math.MinInt32
					}
					return int(NSQ.SLTP_shp_Q14[shp_lag_ind+0]) + int(NSQ.SLTP_shp_Q14[shp_lag_ind-2])
				}
				if ((int(NSQ.SLTP_shp_Q14[shp_lag_ind+0]) | int(NSQ.SLTP_shp_Q14[shp_lag_ind-2])) & 0x80000000) == 0 {
					return math.MaxInt32
				}
				return int(NSQ.SLTP_shp_Q14[shp_lag_ind+0]) + int(NSQ.SLTP_shp_Q14[shp_lag_ind-2])
			}()) * int(int64(int16(HarmShapeFIRPacked_Q14)))) >> 16)
			n_LTP_Q13 = int32(int64(n_LTP_Q13) + ((int64(NSQ.SLTP_shp_Q14[shp_lag_ind-1]) * (int64(HarmShapeFIRPacked_Q14) >> 16)) >> 16))
			n_LTP_Q13 = int32(int(uint32(n_LTP_Q13)) << 1)
			shp_lag_ind++
			tmp2 = int32(int(LTP_pred_Q13) - int(n_LTP_Q13))
			tmp1 = int32(int(tmp2) + int(int32(int(uint32(tmp1))<<1)))
			if 3 == 1 {
				tmp1 = int32((int(tmp1) >> 1) + (int(tmp1) & 1))
			} else {
				tmp1 = int32(((int(tmp1) >> (3 - 1)) + 1) >> 1)
			}
		} else {
			if 2 == 1 {
				tmp1 = int32((int(tmp1) >> 1) + (int(tmp1) & 1))
			} else {
				tmp1 = int32(((int(tmp1) >> (2 - 1)) + 1) >> 1)
			}
		}
		r_Q10 = int32(int(x_sc_Q10[i]) - int(tmp1))
		if int(NSQ.Rand_seed) < 0 {
			r_Q10 = -r_Q10
		}
		if (-(31 << 10)) > (30 << 10) {
			if int(r_Q10) > (-(31 << 10)) {
				r_Q10 = -(31 << 10)
			} else if int(r_Q10) < (30 << 10) {
				r_Q10 = 30 << 10
			} else {
				r_Q10 = r_Q10
			}
		} else if int(r_Q10) > (30 << 10) {
			r_Q10 = 30 << 10
		} else if int(r_Q10) < (-(31 << 10)) {
			r_Q10 = -(31 << 10)
		} else {
			r_Q10 = r_Q10
		}
		q1_Q10 = int32(int(r_Q10) - offset_Q10)
		q1_Q0 = int32(int(q1_Q10) >> 10)
		if Lambda_Q10 > 2048 {
			var rdo_offset int = Lambda_Q10/2 - 512
			if int(q1_Q10) > rdo_offset {
				q1_Q0 = int32((int(q1_Q10) - rdo_offset) >> 10)
			} else if int(q1_Q10) < -rdo_offset {
				q1_Q0 = int32((int(q1_Q10) + rdo_offset) >> 10)
			} else if int(q1_Q10) < 0 {
				q1_Q0 = -1
			} else {
				q1_Q0 = 0
			}
		}
		if int(q1_Q0) > 0 {
			q1_Q10 = int32(int(int32(int(uint32(q1_Q0))<<10)) - QUANT_LEVEL_ADJUST_Q10)
			q1_Q10 = int32(int(q1_Q10) + offset_Q10)
			q2_Q10 = int32(int(q1_Q10) + 1024)
			rd1_Q20 = int32(int(int32(int16(q1_Q10))) * int(int32(int16(Lambda_Q10))))
			rd2_Q20 = int32(int(int32(int16(q2_Q10))) * int(int32(int16(Lambda_Q10))))
		} else if int(q1_Q0) == 0 {
			q1_Q10 = int32(offset_Q10)
			q2_Q10 = int32(int(q1_Q10) + (int(1024 - QUANT_LEVEL_ADJUST_Q10)))
			rd1_Q20 = int32(int(int32(int16(q1_Q10))) * int(int32(int16(Lambda_Q10))))
			rd2_Q20 = int32(int(int32(int16(q2_Q10))) * int(int32(int16(Lambda_Q10))))
		} else if int(q1_Q0) == -1 {
			q2_Q10 = int32(offset_Q10)
			q1_Q10 = int32(int(q2_Q10) - (int(1024 - QUANT_LEVEL_ADJUST_Q10)))
			rd1_Q20 = int32(int(int32(int16(-q1_Q10))) * int(int32(int16(Lambda_Q10))))
			rd2_Q20 = int32(int(int32(int16(q2_Q10))) * int(int32(int16(Lambda_Q10))))
		} else {
			q1_Q10 = int32(int(int32(int(uint32(q1_Q0))<<10)) + QUANT_LEVEL_ADJUST_Q10)
			q1_Q10 = int32(int(q1_Q10) + offset_Q10)
			q2_Q10 = int32(int(q1_Q10) + 1024)
			rd1_Q20 = int32(int(int32(int16(-q1_Q10))) * int(int32(int16(Lambda_Q10))))
			rd2_Q20 = int32(int(int32(int16(-q2_Q10))) * int(int32(int16(Lambda_Q10))))
		}
		rr_Q10 = int32(int(r_Q10) - int(q1_Q10))
		rd1_Q20 = int32(int(rd1_Q20) + int(int32(int16(rr_Q10)))*int(int32(int16(rr_Q10))))
		rr_Q10 = int32(int(r_Q10) - int(q2_Q10))
		rd2_Q20 = int32(int(rd2_Q20) + int(int32(int16(rr_Q10)))*int(int32(int16(rr_Q10))))
		if int(rd2_Q20) < int(rd1_Q20) {
			q1_Q10 = q2_Q10
		}
		if 10 == 1 {
			pulses[i] = int8((int(q1_Q10) >> 1) + (int(q1_Q10) & 1))
		} else {
			pulses[i] = int8(((int(q1_Q10) >> (10 - 1)) + 1) >> 1)
		}
		exc_Q14 = int32(int(uint32(q1_Q10)) << 4)
		if int(NSQ.Rand_seed) < 0 {
			exc_Q14 = -exc_Q14
		}
		LPC_exc_Q14 = int32(int(exc_Q14) + int(int32(int(uint32(LTP_pred_Q13))<<1)))
		xq_Q14 = int32(int(LPC_exc_Q14) + int(int32(int(uint32(LPC_pred_Q10))<<4)))
		if (func() int {
			if 8 == 1 {
				return (int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) >> 1) + (int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) & 1)
			}
			return ((int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			xq[i] = math.MaxInt16
		} else if (func() int {
			if 8 == 1 {
				return (int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) >> 1) + (int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) & 1)
			}
			return ((int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			xq[i] = math.MinInt16
		} else if 8 == 1 {
			xq[i] = int16((int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) >> 1) + (int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) & 1))
		} else {
			xq[i] = int16(((int(int32((int64(xq_Q14)*int64(Gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1)
		}
		psLPC_Q14_ptr = psLPC_Q14_ptr[1:]
		psLPC_Q14_ind++
		psLPC_Q14_ptr[0] = xq_Q14
		NSQ.SDiff_shp_Q14 = int32(int(xq_Q14) - int(int32(int(uint32(x_sc_Q10[i]))<<4)))
		sLF_AR_shp_Q14 = int32(int(NSQ.SDiff_shp_Q14) - int(int32(int(uint32(n_AR_Q12))<<2)))
		NSQ.SLF_AR_shp_Q14 = sLF_AR_shp_Q14
		NSQ.SLTP_shp_Q14[NSQ.SLTP_shp_buf_idx] = int32(int(sLF_AR_shp_Q14) - int(int32(int(uint32(n_LF_Q12))<<2)))
		sLTP_Q15[NSQ.SLTP_buf_idx] = int32(int(uint32(LPC_exc_Q14)) << 1)
		NSQ.SLTP_shp_buf_idx++
		NSQ.SLTP_buf_idx++
		NSQ.Rand_seed = int32(int(uint32(NSQ.Rand_seed)) + int(uint32(pulses[i])))
	}
	libc.MemCpy(unsafe.Pointer(&NSQ.SLPC_Q14[0]), unsafe.Pointer(&NSQ.SLPC_Q14[length]), int(MAX_LPC_ORDER*unsafe.Sizeof(int32(0))))
}
func silk_nsq_scale_states(psEncC *EncoderState, NSQ *NSQState, x16 []int16, x_sc_Q10 []int32, sLTP []int16, sLTP_Q15 []int32, subfr int, LTP_scale_Q14 int, Gains_Q16 [4]int32, pitchL [4]int, signal_type int) {
	var (
		i            int
		lag          int
		gain_adj_Q16 int32
		inv_gain_Q31 int32
		inv_gain_Q26 int32
	)
	lag = pitchL[subfr]
	inv_gain_Q31 = silk_INVERSE32_varQ(int32(func() int {
		if int(Gains_Q16[subfr]) > 1 {
			return int(Gains_Q16[subfr])
		}
		return 1
	}()), 47)
	if 5 == 1 {
		inv_gain_Q26 = int32((int(inv_gain_Q31) >> 1) + (int(inv_gain_Q31) & 1))
	} else {
		inv_gain_Q26 = int32(((int(inv_gain_Q31) >> (5 - 1)) + 1) >> 1)
	}
	for i = 0; i < psEncC.Subfr_length; i++ {
		x_sc_Q10[i] = int32((int64(x16[i]) * int64(inv_gain_Q26)) >> 16)
	}
	if NSQ.Rewhite_flag != 0 {
		if subfr == 0 {
			inv_gain_Q31 = int32(int(uint32(int32((int64(inv_gain_Q31)*int64(int16(LTP_scale_Q14)))>>16))) << 2)
		}
		for i = NSQ.SLTP_buf_idx - lag - int(LTP_ORDER/2); i < NSQ.SLTP_buf_idx; i++ {
			sLTP_Q15[i] = int32((int64(inv_gain_Q31) * int64(sLTP[i])) >> 16)
		}
	}
	if int(Gains_Q16[subfr]) != int(NSQ.Prev_gain_Q16) {
		gain_adj_Q16 = silk_DIV32_varQ(NSQ.Prev_gain_Q16, Gains_Q16[subfr], 16)
		for i = NSQ.SLTP_shp_buf_idx - psEncC.Ltp_mem_length; i < NSQ.SLTP_shp_buf_idx; i++ {
			NSQ.SLTP_shp_Q14[i] = int32((int64(gain_adj_Q16) * int64(NSQ.SLTP_shp_Q14[i])) >> 16)
		}
		if signal_type == TYPE_VOICED && NSQ.Rewhite_flag == 0 {
			for i = NSQ.SLTP_buf_idx - lag - int(LTP_ORDER/2); i < NSQ.SLTP_buf_idx; i++ {
				sLTP_Q15[i] = int32((int64(gain_adj_Q16) * int64(sLTP_Q15[i])) >> 16)
			}
		}
		NSQ.SLF_AR_shp_Q14 = int32((int64(gain_adj_Q16) * int64(NSQ.SLF_AR_shp_Q14)) >> 16)
		NSQ.SDiff_shp_Q14 = int32((int64(gain_adj_Q16) * int64(NSQ.SDiff_shp_Q14)) >> 16)
		for i = 0; i < MAX_LPC_ORDER; i++ {
			NSQ.SLPC_Q14[i] = int32((int64(gain_adj_Q16) * int64(NSQ.SLPC_Q14[i])) >> 16)
		}
		for i = 0; i < MAX_SHAPE_LPC_ORDER; i++ {
			NSQ.SAR2_Q14[i] = int32((int64(gain_adj_Q16) * int64(NSQ.SAR2_Q14[i])) >> 16)
		}
		NSQ.Prev_gain_Q16 = Gains_Q16[subfr]
	}
}
