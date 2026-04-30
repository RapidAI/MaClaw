package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

type NSQ_del_dec_struct struct {
	SLPC_Q14  [96]int32
	RandState [40]int32
	Q_Q10     [40]int32
	Xq_Q14    [40]int32
	Pred_Q15  [40]int32
	Shape_Q14 [40]int32
	SAR2_Q14  [24]int32
	LF_AR_Q14 int32
	Diff_Q14  int32
	Seed      int32
	SeedInit  int32
	RD_Q10    int32
}
type NSQ_sample_struct struct {
	Q_Q10        int32
	RD_Q10       int32
	Xq_Q14       int32
	LF_AR_Q14    int32
	Diff_Q14     int32
	SLTP_shp_Q14 int32
	LPC_exc_Q14  int32
}
type NSQ_sample_pair [2]NSQ_sample_struct

func NSQ_del_dec_c(psEncC *EncoderState, NSQ *NSQState, psIndices *SideInfoIndices, x16 []int16, pulses []int8, PredCoef_Q12 [32]int16, LTPCoef_Q14 [20]int16, AR_Q13 [96]int16, HarmShapeGain_Q14 [4]int, Tilt_Q14 [4]int, LF_shp_Q14 [4]int32, Gains_Q16 [4]int32, pitchL [4]int, Lambda_Q10 int, LTP_scale_Q14 int) {
	var (
		i                      int
		k                      int
		lag                    int
		start_idx              int
		LSF_interpolation_flag int
		Winner_ind             int
		subfr                  int
		last_smple_idx         int
		smpl_buf_idx           int
		decisionDelay          int
		A_Q12                  []int16
		B_Q14                  []int16
		AR_shp_Q13             []int16
		pxq                    []int16
		HarmShapeFIRPacked_Q14 int32
		offset_Q10             int
		RDmin_Q10              int32
		Gain_Q10               int32
	)
	lag = NSQ.LagPrev
	psDelDec := make([]NSQ_del_dec_struct, psEncC.NStatesDelayedDecision)
	for k = 0; k < psEncC.NStatesDelayedDecision; k++ {
		psDD := &psDelDec[k]
		psDD.Seed = int32((k + int(psIndices.Seed)) & 3)
		psDD.SeedInit = psDD.Seed
		psDD.RD_Q10 = 0
		psDD.LF_AR_Q14 = NSQ.SLF_AR_shp_Q14
		psDD.Diff_Q14 = NSQ.SDiff_shp_Q14
		psDD.Shape_Q14[0] = NSQ.SLTP_shp_Q14[psEncC.Ltp_mem_length-1]
		libc.MemCpy(unsafe.Pointer(&psDD.SLPC_Q14[0]), unsafe.Pointer(&NSQ.SLPC_Q14[0]), int(MAX_LPC_ORDER*unsafe.Sizeof(int32(0))))
		libc.MemCpy(unsafe.Pointer(&psDD.SAR2_Q14[0]), unsafe.Pointer(&NSQ.SAR2_Q14[0]), int(unsafe.Sizeof([24]int32{})))
	}
	offset_Q10 = int(silk_Quantization_Offsets_Q10[int(psIndices.SignalType)>>1][psIndices.QuantOffsetType])
	smpl_buf_idx = 0
	decisionDelay = silk_min_int(DECISION_DELAY, psEncC.Subfr_length)
	if int(psIndices.SignalType) == TYPE_VOICED {
		for k = 0; k < psEncC.Nb_subfr; k++ {
			decisionDelay = silk_min_int(decisionDelay, pitchL[k]-int(LTP_ORDER/2)-1)
		}
	} else {
		if lag > 0 {
			decisionDelay = silk_min_int(decisionDelay, lag-int(LTP_ORDER/2)-1)
		}
	}
	if int(psIndices.NLSFInterpCoef_Q2) == 4 {
		LSF_interpolation_flag = 0
	} else {
		LSF_interpolation_flag = 1
	}
	sLTP_Q15 := make([]int32, psEncC.Ltp_mem_length+psEncC.Frame_length)
	sLTP := make([]int16, psEncC.Ltp_mem_length+psEncC.Frame_length)
	x_sc_Q10 := make([]int32, psEncC.Subfr_length)
	delayedGain_Q10 := make([]int32, DECISION_DELAY)
	pxq = NSQ.Xq[psEncC.Ltp_mem_length:]
	NSQ.SLTP_shp_buf_idx = psEncC.Ltp_mem_length
	NSQ.SLTP_buf_idx = psEncC.Ltp_mem_length
	subfr = 0
	for k = 0; k < psEncC.Nb_subfr; k++ {
		A_Q12 = PredCoef_Q12[((k>>1)|(1-LSF_interpolation_flag))*MAX_LPC_ORDER:]
		B_Q14 = LTPCoef_Q14[k*LTP_ORDER:]
		AR_shp_Q13 = AR_Q13[k*MAX_SHAPE_LPC_ORDER:]
		HarmShapeFIRPacked_Q14 = int32((HarmShapeGain_Q14[k]) >> 2)
		HarmShapeFIRPacked_Q14 |= int32(int(uint32(int32((HarmShapeGain_Q14[k])>>1))) << 16)
		NSQ.Rewhite_flag = 0
		if int(psIndices.SignalType) == TYPE_VOICED {
			lag = pitchL[k]
			if (k & (3 - int(int32(int(uint32(int32(LSF_interpolation_flag)))<<1)))) == 0 {
				if k == 2 {
					RDmin_Q10 = psDelDec[0].RD_Q10
					Winner_ind = 0
					for i = 1; i < psEncC.NStatesDelayedDecision; i++ {
						if int(psDelDec[i].RD_Q10) < int(RDmin_Q10) {
							RDmin_Q10 = psDelDec[i].RD_Q10
							Winner_ind = i
						}
					}
					for i = 0; i < psEncC.NStatesDelayedDecision; i++ {
						if i != Winner_ind {
							psDelDec[i].RD_Q10 += int32(int(math.MaxInt32 >> 4))
						}
					}
					psDD := &psDelDec[Winner_ind]
					last_smple_idx = smpl_buf_idx + decisionDelay
					pulsesNeg := sliceFromNegInt8(pulses, decisionDelay)
					pxqNeg := sliceFromNegInt16(pxq, decisionDelay)
					for i = 0; i < decisionDelay; i++ {
						last_smple_idx = (last_smple_idx - 1) % DECISION_DELAY
						if last_smple_idx < 0 {
							last_smple_idx += DECISION_DELAY
						}
						if 10 == 1 {
							pulsesNeg[i] = int8((int(psDD.Q_Q10[last_smple_idx]) >> 1) + (int(psDD.Q_Q10[last_smple_idx]) & 1))
						} else {
							pulsesNeg[i] = int8(((int(psDD.Q_Q10[last_smple_idx]) >> (10 - 1)) + 1) >> 1)
						}
						if (func() int {
							if 14 == 1 {
								return (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) & 1)
							}
							return ((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) >> (14 - 1)) + 1) >> 1
						}()) > math.MaxInt16 {
							pxqNeg[i] = math.MaxInt16
						} else if (func() int {
							if 14 == 1 {
								return (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) & 1)
							}
							return ((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) >> (14 - 1)) + 1) >> 1
						}()) < int(math.MinInt16) {
							pxqNeg[i] = math.MinInt16
						} else if 14 == 1 {
							pxqNeg[i] = int16((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) & 1))
						} else {
							pxqNeg[i] = int16(((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gains_Q16[1]))>>16)) >> (14 - 1)) + 1) >> 1)
						}
						NSQ.SLTP_shp_Q14[NSQ.SLTP_shp_buf_idx-decisionDelay+i] = psDD.Shape_Q14[last_smple_idx]
					}
					subfr = 0
				}
				start_idx = psEncC.Ltp_mem_length - lag - psEncC.PredictLPCOrder - int(LTP_ORDER/2)
				silk_LPC_analysis_filter(sLTP[start_idx:], NSQ.Xq[start_idx+k*psEncC.Subfr_length:], []int16(A_Q12), int32(psEncC.Ltp_mem_length-start_idx), int32(psEncC.PredictLPCOrder), psEncC.Arch)
				NSQ.SLTP_buf_idx = psEncC.Ltp_mem_length
				NSQ.Rewhite_flag = 1
			}
		}
		silk_nsq_del_dec_scale_states(psEncC, NSQ, []NSQ_del_dec_struct(psDelDec), x16, []int32(x_sc_Q10), []int16(sLTP), []int32(sLTP_Q15), k, psEncC.NStatesDelayedDecision, LTP_scale_Q14, Gains_Q16, pitchL, int(psIndices.SignalType), decisionDelay)
		silk_noise_shape_quantizer_del_dec(NSQ, []NSQ_del_dec_struct(psDelDec), int(psIndices.SignalType), []int32(x_sc_Q10), pulses, []int16(pxq), []int32(sLTP_Q15), []int32(delayedGain_Q10), []int16(A_Q12), []int16(B_Q14), []int16(AR_shp_Q13), lag, HarmShapeFIRPacked_Q14, Tilt_Q14[k], LF_shp_Q14[k], Gains_Q16[k], Lambda_Q10, offset_Q10, psEncC.Subfr_length, func() int {
			p := &subfr
			x := *p
			*p++
			return x
		}(), psEncC.ShapingLPCOrder, psEncC.PredictLPCOrder, psEncC.Warping_Q16, psEncC.NStatesDelayedDecision, &smpl_buf_idx, decisionDelay, psEncC.Arch)
		x16 = x16[psEncC.Subfr_length:]
		pulses = pulses[psEncC.Subfr_length:]
		pxq = pxq[psEncC.Subfr_length:]
	}
	RDmin_Q10 = psDelDec[0].RD_Q10
	Winner_ind = 0
	for k = 1; k < psEncC.NStatesDelayedDecision; k++ {
		if int(psDelDec[k].RD_Q10) < int(RDmin_Q10) {
			RDmin_Q10 = psDelDec[k].RD_Q10
			Winner_ind = k
		}
	}
	psDD := &psDelDec[Winner_ind]
	psIndices.Seed = int8(psDD.SeedInit)
	last_smple_idx = smpl_buf_idx + decisionDelay
	Gain_Q10 = int32(int(Gains_Q16[psEncC.Nb_subfr-1]) >> 6)
	pulsesNeg := sliceFromNegInt8(pulses, decisionDelay)
	pxqNeg := sliceFromNegInt16(pxq, decisionDelay)
	for i = 0; i < decisionDelay; i++ {
		last_smple_idx = (last_smple_idx - 1) % DECISION_DELAY
		if last_smple_idx < 0 {
			last_smple_idx += DECISION_DELAY
		}
		if 10 == 1 {
			pulsesNeg[i] = int8((int(psDD.Q_Q10[last_smple_idx]) >> 1) + (int(psDD.Q_Q10[last_smple_idx]) & 1))
		} else {
			pulsesNeg[i] = int8(((int(psDD.Q_Q10[last_smple_idx]) >> (10 - 1)) + 1) >> 1)
		}
		if (func() int {
			if 8 == 1 {
				return (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) & 1)
			}
			return ((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
		}()) > math.MaxInt16 {
			pxqNeg[i] = math.MaxInt16
		} else if (func() int {
			if 8 == 1 {
				return (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) & 1)
			}
			return ((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1
		}()) < int(math.MinInt16) {
			pxqNeg[i] = math.MinInt16
		} else if 8 == 1 {
			pxqNeg[i] = int16((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) & 1))
		} else {
			pxqNeg[i] = int16(((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(Gain_Q10))>>16)) >> (8 - 1)) + 1) >> 1)
		}
		NSQ.SLTP_shp_Q14[NSQ.SLTP_shp_buf_idx-decisionDelay+i] = psDD.Shape_Q14[last_smple_idx]
	}
	libc.MemCpy(unsafe.Pointer(&NSQ.SLPC_Q14[0]), unsafe.Pointer(&psDD.SLPC_Q14[psEncC.Subfr_length]), int(MAX_LPC_ORDER*unsafe.Sizeof(int32(0))))
	libc.MemCpy(unsafe.Pointer(&NSQ.SAR2_Q14[0]), unsafe.Pointer(&psDD.SAR2_Q14[0]), int(unsafe.Sizeof([24]int32{})))
	NSQ.SLF_AR_shp_Q14 = psDD.LF_AR_Q14
	NSQ.SDiff_shp_Q14 = psDD.Diff_Q14
	NSQ.LagPrev = pitchL[psEncC.Nb_subfr-1]
	libc.MemMove(unsafe.Pointer(&NSQ.Xq[0]), unsafe.Pointer(&NSQ.Xq[psEncC.Frame_length]), psEncC.Ltp_mem_length*int(unsafe.Sizeof(int16(0))))
	libc.MemMove(unsafe.Pointer(&NSQ.SLTP_shp_Q14[0]), unsafe.Pointer(&NSQ.SLTP_shp_Q14[psEncC.Frame_length]), psEncC.Ltp_mem_length*int(unsafe.Sizeof(int32(0))))
}
func silk_noise_shape_quantizer_del_dec(NSQ *NSQState, psDelDec []NSQ_del_dec_struct, signalType int, x_Q10 []int32, pulses []int8, xq []int16, sLTP_Q15 []int32, delayedGain_Q10 []int32, a_Q12 []int16, b_Q14 []int16, AR_shp_Q13 []int16, lag int, HarmShapeFIRPacked_Q14 int32, Tilt_Q14 int, LF_shp_Q14 int32, Gain_Q16 int32, Lambda_Q10 int, offset_Q10 int, length int, subfr int, shapingLPCOrder int, predictLPCOrder int, warping_Q16 int, nStatesDelayedDecision int, smpl_buf_idx *int, decisionDelay int, arch int) {
	var (
		i                 int
		j                 int
		k                 int
		Winner_ind        int
		RDmin_ind         int
		RDmax_ind         int
		last_smple_idx    int
		Winner_rand_state int32
		LTP_pred_Q14      int32
		LPC_pred_Q14      int32
		n_AR_Q14          int32
		n_LTP_Q14         int32
		n_LF_Q14          int32
		r_Q10             int32
		rr_Q10            int32
		rd1_Q10           int32
		rd2_Q10           int32
		RDmin_Q10         int32
		RDmax_Q10         int32
		q1_Q0             int32
		q1_Q10            int32
		q2_Q10            int32
		exc_Q14           int32
		LPC_exc_Q14       int32
		xq_Q14            int32
		Gain_Q10          int32
		tmp1              int32
		tmp2              int32
		sLF_AR_shp_Q14    int32
		psDD              *NSQ_del_dec_struct
	)
	psSampleState := make([]NSQ_sample_pair, nStatesDelayedDecision)
	shp_lag_ind := NSQ.SLTP_shp_buf_idx - lag + int(HARM_SHAPE_FIR_TAPS/2)
	pred_lag_ind := NSQ.SLTP_buf_idx - lag + int(LTP_ORDER/2)
	// Create negative-index-capable views for C-style pointer arithmetic.
	// In C, pulses and xq are pointers that can be indexed negatively
	// (pointing into previously-processed subframe data).
	pulsesNeg := sliceFromNegInt8(pulses, decisionDelay)
	xqNeg := sliceFromNegInt16(xq, decisionDelay)
	Gain_Q10 = int32(int(Gain_Q16) >> 6)
	for i = 0; i < length; i++ {
		if signalType == TYPE_VOICED {
			LTP_pred_Q14 = 2
			LTP_pred_Q14 = int32(int64(LTP_pred_Q14) + ((int64(sLTP_Q15[pred_lag_ind+0]) * int64(b_Q14[0])) >> 16))
			LTP_pred_Q14 = int32(int64(LTP_pred_Q14) + ((int64(sLTP_Q15[pred_lag_ind-1]) * int64(b_Q14[1])) >> 16))
			LTP_pred_Q14 = int32(int64(LTP_pred_Q14) + ((int64(sLTP_Q15[pred_lag_ind-2]) * int64(b_Q14[2])) >> 16))
			LTP_pred_Q14 = int32(int64(LTP_pred_Q14) + ((int64(sLTP_Q15[pred_lag_ind-3]) * int64(b_Q14[3])) >> 16))
			LTP_pred_Q14 = int32(int64(LTP_pred_Q14) + ((int64(sLTP_Q15[pred_lag_ind-4]) * int64(b_Q14[4])) >> 16))
			LTP_pred_Q14 = int32(int(uint32(LTP_pred_Q14)) << 1)
			pred_lag_ind++
		} else {
			LTP_pred_Q14 = 0
		}
		if lag > 0 {
			n_LTP_Q14 = int32(((func() int {
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
			n_LTP_Q14 = int32(int64(n_LTP_Q14) + ((int64(NSQ.SLTP_shp_Q14[shp_lag_ind-1]) * (int64(HarmShapeFIRPacked_Q14) >> 16)) >> 16))
			n_LTP_Q14 = int32(int(LTP_pred_Q14) - int(int32(int(uint32(n_LTP_Q14))<<2)))
			shp_lag_ind++
		} else {
			n_LTP_Q14 = 0
		}
		for k = 0; k < nStatesDelayedDecision; k++ {
			psDD = &psDelDec[k]
			psSS := psSampleState[k][:]
			psDD.Seed = int32(RAND_INCREMENT + int(uint32(int32(int(uint32(psDD.Seed))*RAND_MULTIPLIER))))
			LPC_pred_Q14 = func() int32 {
				_ = arch
				return silk_noise_shape_quantizer_short_prediction_c(psDD.SLPC_Q14[:], MAX_LPC_ORDER-1+i, a_Q12[:], predictLPCOrder)
			}()
			LPC_pred_Q14 = int32(int(uint32(LPC_pred_Q14)) << 4)
			tmp2 = int32(int64(psDD.Diff_Q14) + ((int64(psDD.SAR2_Q14[0]) * int64(int16(warping_Q16))) >> 16))
			tmp1 = int32(int(psDD.SAR2_Q14[0]) + (((int(psDD.SAR2_Q14[1]) - int(tmp2)) * int(int64(int16(warping_Q16)))) >> 16))
			psDD.SAR2_Q14[0] = tmp2
			n_AR_Q14 = int32(shapingLPCOrder >> 1)
			n_AR_Q14 = int32(int64(n_AR_Q14) + ((int64(tmp2) * int64(AR_shp_Q13[0])) >> 16))
			for j = 2; j < shapingLPCOrder; j += 2 {
				tmp2 = int32(int(psDD.SAR2_Q14[j-1]) + (((int(psDD.SAR2_Q14[j+0]) - int(tmp1)) * int(int64(int16(warping_Q16)))) >> 16))
				psDD.SAR2_Q14[j-1] = tmp1
				n_AR_Q14 = int32(int64(n_AR_Q14) + ((int64(tmp1) * int64(AR_shp_Q13[j-1])) >> 16))
				tmp1 = int32(int(psDD.SAR2_Q14[j+0]) + (((int(psDD.SAR2_Q14[j+1]) - int(tmp2)) * int(int64(int16(warping_Q16)))) >> 16))
				psDD.SAR2_Q14[j+0] = tmp2
				n_AR_Q14 = int32(int64(n_AR_Q14) + ((int64(tmp2) * int64(AR_shp_Q13[j])) >> 16))
			}
			psDD.SAR2_Q14[shapingLPCOrder-1] = tmp1
			n_AR_Q14 = int32(int64(n_AR_Q14) + ((int64(tmp1) * int64(AR_shp_Q13[shapingLPCOrder-1])) >> 16))
			n_AR_Q14 = int32(int(uint32(n_AR_Q14)) << 1)
			n_AR_Q14 = int32(int64(n_AR_Q14) + ((int64(psDD.LF_AR_Q14) * int64(int16(Tilt_Q14))) >> 16))
			n_AR_Q14 = int32(int(uint32(n_AR_Q14)) << 2)
			n_LF_Q14 = int32((int64(psDD.Shape_Q14[*smpl_buf_idx]) * int64(int16(LF_shp_Q14))) >> 16)
			n_LF_Q14 = int32(int64(n_LF_Q14) + ((int64(psDD.LF_AR_Q14) * (int64(LF_shp_Q14) >> 16)) >> 16))
			n_LF_Q14 = int32(int(uint32(n_LF_Q14)) << 2)
			if ((int(uint32(n_AR_Q14)) + int(uint32(n_LF_Q14))) & 0x80000000) == 0 {
				if ((int(n_AR_Q14) & int(n_LF_Q14)) & 0x80000000) != 0 {
					tmp1 = math.MinInt32
				} else {
					tmp1 = int32(int(n_AR_Q14) + int(n_LF_Q14))
				}
			} else if ((int(n_AR_Q14) | int(n_LF_Q14)) & 0x80000000) == 0 {
				tmp1 = math.MaxInt32
			} else {
				tmp1 = int32(int(n_AR_Q14) + int(n_LF_Q14))
			}
			tmp2 = int32(int(n_LTP_Q14) + int(LPC_pred_Q14))
			if ((int(uint32(tmp2)) - int(uint32(tmp1))) & 0x80000000) == 0 {
				if (int(tmp2) & (int(tmp1) ^ 0x80000000) & 0x80000000) != 0 {
					tmp1 = math.MinInt32
				} else {
					tmp1 = int32(int(tmp2) - int(tmp1))
				}
			} else if ((int(tmp2) ^ 0x80000000) & int(tmp1) & 0x80000000) != 0 {
				tmp1 = math.MaxInt32
			} else {
				tmp1 = int32(int(tmp2) - int(tmp1))
			}
			if 4 == 1 {
				tmp1 = int32((int(tmp1) >> 1) + (int(tmp1) & 1))
			} else {
				tmp1 = int32(((int(tmp1) >> (4 - 1)) + 1) >> 1)
			}
			r_Q10 = int32(int(x_Q10[i]) - int(tmp1))
			if int(psDD.Seed) < 0 {
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
				rd1_Q10 = int32(int(int32(int16(q1_Q10))) * int(int32(int16(Lambda_Q10))))
				rd2_Q10 = int32(int(int32(int16(q2_Q10))) * int(int32(int16(Lambda_Q10))))
			} else if int(q1_Q0) == 0 {
				q1_Q10 = int32(offset_Q10)
				q2_Q10 = int32(int(q1_Q10) + (int(1024 - QUANT_LEVEL_ADJUST_Q10)))
				rd1_Q10 = int32(int(int32(int16(q1_Q10))) * int(int32(int16(Lambda_Q10))))
				rd2_Q10 = int32(int(int32(int16(q2_Q10))) * int(int32(int16(Lambda_Q10))))
			} else if int(q1_Q0) == -1 {
				q2_Q10 = int32(offset_Q10)
				q1_Q10 = int32(int(q2_Q10) - (int(1024 - QUANT_LEVEL_ADJUST_Q10)))
				rd1_Q10 = int32(int(int32(int16(-q1_Q10))) * int(int32(int16(Lambda_Q10))))
				rd2_Q10 = int32(int(int32(int16(q2_Q10))) * int(int32(int16(Lambda_Q10))))
			} else {
				q1_Q10 = int32(int(int32(int(uint32(q1_Q0))<<10)) + QUANT_LEVEL_ADJUST_Q10)
				q1_Q10 = int32(int(q1_Q10) + offset_Q10)
				q2_Q10 = int32(int(q1_Q10) + 1024)
				rd1_Q10 = int32(int(int32(int16(-q1_Q10))) * int(int32(int16(Lambda_Q10))))
				rd2_Q10 = int32(int(int32(int16(-q2_Q10))) * int(int32(int16(Lambda_Q10))))
			}
			rr_Q10 = int32(int(r_Q10) - int(q1_Q10))
			rd1_Q10 = int32((int(rd1_Q10) + int(int32(int16(rr_Q10)))*int(int32(int16(rr_Q10)))) >> 10)
			rr_Q10 = int32(int(r_Q10) - int(q2_Q10))
			rd2_Q10 = int32((int(rd2_Q10) + int(int32(int16(rr_Q10)))*int(int32(int16(rr_Q10)))) >> 10)
			if int(rd1_Q10) < int(rd2_Q10) {
				psSS[0].RD_Q10 = int32(int(psDD.RD_Q10) + int(rd1_Q10))
				psSS[1].RD_Q10 = int32(int(psDD.RD_Q10) + int(rd2_Q10))
				psSS[0].Q_Q10 = q1_Q10
				psSS[1].Q_Q10 = q2_Q10
			} else {
				psSS[0].RD_Q10 = int32(int(psDD.RD_Q10) + int(rd2_Q10))
				psSS[1].RD_Q10 = int32(int(psDD.RD_Q10) + int(rd1_Q10))
				psSS[0].Q_Q10 = q2_Q10
				psSS[1].Q_Q10 = q1_Q10
			}
			exc_Q14 = int32(int(uint32(psSS[0].Q_Q10)) << 4)
			if int(psDD.Seed) < 0 {
				exc_Q14 = -exc_Q14
			}
			LPC_exc_Q14 = int32(int(exc_Q14) + int(LTP_pred_Q14))
			xq_Q14 = int32(int(LPC_exc_Q14) + int(LPC_pred_Q14))
			psSS[0].Diff_Q14 = int32(int(xq_Q14) - int(int32(int(uint32(x_Q10[i]))<<4)))
			sLF_AR_shp_Q14 = int32(int(psSS[0].Diff_Q14) - int(n_AR_Q14))
			if ((int(uint32(sLF_AR_shp_Q14)) - int(uint32(n_LF_Q14))) & 0x80000000) == 0 {
				if (int(sLF_AR_shp_Q14) & (int(n_LF_Q14) ^ 0x80000000) & 0x80000000) != 0 {
					psSS[0].SLTP_shp_Q14 = math.MinInt32
				} else {
					psSS[0].SLTP_shp_Q14 = int32(int(sLF_AR_shp_Q14) - int(n_LF_Q14))
				}
			} else if ((int(sLF_AR_shp_Q14) ^ 0x80000000) & int(n_LF_Q14) & 0x80000000) != 0 {
				psSS[0].SLTP_shp_Q14 = math.MaxInt32
			} else {
				psSS[0].SLTP_shp_Q14 = int32(int(sLF_AR_shp_Q14) - int(n_LF_Q14))
			}
			psSS[0].LF_AR_Q14 = sLF_AR_shp_Q14
			psSS[0].LPC_exc_Q14 = LPC_exc_Q14
			psSS[0].Xq_Q14 = xq_Q14
			exc_Q14 = int32(int(uint32(psSS[1].Q_Q10)) << 4)
			if int(psDD.Seed) < 0 {
				exc_Q14 = -exc_Q14
			}
			LPC_exc_Q14 = int32(int(exc_Q14) + int(LTP_pred_Q14))
			xq_Q14 = int32(int(LPC_exc_Q14) + int(LPC_pred_Q14))
			psSS[1].Diff_Q14 = int32(int(xq_Q14) - int(int32(int(uint32(x_Q10[i]))<<4)))
			sLF_AR_shp_Q14 = int32(int(psSS[1].Diff_Q14) - int(n_AR_Q14))
			if ((int(uint32(sLF_AR_shp_Q14)) - int(uint32(n_LF_Q14))) & 0x80000000) == 0 {
				if (int(sLF_AR_shp_Q14) & (int(n_LF_Q14) ^ 0x80000000) & 0x80000000) != 0 {
					psSS[1].SLTP_shp_Q14 = math.MinInt32
				} else {
					psSS[1].SLTP_shp_Q14 = int32(int(sLF_AR_shp_Q14) - int(n_LF_Q14))
				}
			} else if ((int(sLF_AR_shp_Q14) ^ 0x80000000) & int(n_LF_Q14) & 0x80000000) != 0 {
				psSS[1].SLTP_shp_Q14 = math.MaxInt32
			} else {
				psSS[1].SLTP_shp_Q14 = int32(int(sLF_AR_shp_Q14) - int(n_LF_Q14))
			}
			psSS[1].LF_AR_Q14 = sLF_AR_shp_Q14
			psSS[1].LPC_exc_Q14 = LPC_exc_Q14
			psSS[1].Xq_Q14 = xq_Q14
		}
		*smpl_buf_idx = (*smpl_buf_idx - 1) % DECISION_DELAY
		if *smpl_buf_idx < 0 {
			*smpl_buf_idx += DECISION_DELAY
		}
		last_smple_idx = (*smpl_buf_idx + decisionDelay) % DECISION_DELAY
		RDmin_Q10 = psSampleState[0][0].RD_Q10
		Winner_ind = 0
		for k = 1; k < nStatesDelayedDecision; k++ {
			if int(psSampleState[k][0].RD_Q10) < int(RDmin_Q10) {
				RDmin_Q10 = psSampleState[k][0].RD_Q10
				Winner_ind = k
			}
		}
		Winner_rand_state = psDelDec[Winner_ind].RandState[last_smple_idx]
		for k = 0; k < nStatesDelayedDecision; k++ {
			if int(psDelDec[k].RandState[last_smple_idx]) != int(Winner_rand_state) {
				psSampleState[k][0].RD_Q10 = int32(int(psSampleState[k][0].RD_Q10) + (int(math.MaxInt32 >> 4)))
				psSampleState[k][1].RD_Q10 = int32(int(psSampleState[k][1].RD_Q10) + (int(math.MaxInt32 >> 4)))
			}
		}
		RDmax_Q10 = psSampleState[0][0].RD_Q10
		RDmin_Q10 = psSampleState[0][1].RD_Q10
		RDmax_ind = 0
		RDmin_ind = 0
		for k = 1; k < nStatesDelayedDecision; k++ {
			if int(psSampleState[k][0].RD_Q10) > int(RDmax_Q10) {
				RDmax_Q10 = psSampleState[k][0].RD_Q10
				RDmax_ind = k
			}
			if int(psSampleState[k][1].RD_Q10) < int(RDmin_Q10) {
				RDmin_Q10 = psSampleState[k][1].RD_Q10
				RDmin_ind = k
			}
		}
		if int(RDmin_Q10) < int(RDmax_Q10) {
			libc.MemCpy(unsafe.Pointer(&psDelDec[RDmax_ind].SLPC_Q14[i]), unsafe.Pointer(&psDelDec[RDmin_ind].SLPC_Q14[i]), int(unsafe.Sizeof(NSQ_del_dec_struct{})-uintptr(i*int(unsafe.Sizeof(int32(0))))))
			psSampleState[RDmax_ind][0] = psSampleState[RDmin_ind][1]
		}
		psDD = &psDelDec[Winner_ind]
		if subfr > 0 || i >= decisionDelay {
			if 10 == 1 {
				pulsesNeg[i] = int8((int(psDD.Q_Q10[last_smple_idx]) >> 1) + (int(psDD.Q_Q10[last_smple_idx]) & 1))
			} else {
				pulsesNeg[i] = int8(((int(psDD.Q_Q10[last_smple_idx]) >> (10 - 1)) + 1) >> 1)
			}
			if (func() int {
				if 8 == 1 {
					return (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) & 1)
				}
				return ((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) >> (8 - 1)) + 1) >> 1
			}()) > math.MaxInt16 {
				xqNeg[i] = math.MaxInt16
			} else if (func() int {
				if 8 == 1 {
					return (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) & 1)
				}
				return ((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) >> (8 - 1)) + 1) >> 1
			}()) < int(math.MinInt16) {
				xqNeg[i] = math.MinInt16
			} else if 8 == 1 {
				xqNeg[i] = int16((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) >> 1) + (int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) & 1))
			} else {
				xqNeg[i] = int16(((int(int32((int64(psDD.Xq_Q14[last_smple_idx])*int64(delayedGain_Q10[last_smple_idx]))>>16)) >> (8 - 1)) + 1) >> 1)
			}
			NSQ.SLTP_shp_Q14[NSQ.SLTP_shp_buf_idx-decisionDelay] = psDD.Shape_Q14[last_smple_idx]
			sLTP_Q15[NSQ.SLTP_buf_idx-decisionDelay] = psDD.Pred_Q15[last_smple_idx]
		}
		NSQ.SLTP_shp_buf_idx++
		NSQ.SLTP_buf_idx++
		for k = 0; k < nStatesDelayedDecision; k++ {
			psDD = &psDelDec[k]
			psSS := &psSampleState[k][0]
			psDD.LF_AR_Q14 = psSS.LF_AR_Q14
			psDD.Diff_Q14 = psSS.Diff_Q14
			psDD.SLPC_Q14[MAX_LPC_ORDER+i] = psSS.Xq_Q14
			psDD.Xq_Q14[*smpl_buf_idx] = psSS.Xq_Q14
			psDD.Q_Q10[*smpl_buf_idx] = psSS.Q_Q10
			psDD.Pred_Q15[*smpl_buf_idx] = int32(int(uint32(psSS.LPC_exc_Q14)) << 1)
			psDD.Shape_Q14[*smpl_buf_idx] = psSS.SLTP_shp_Q14
			psDD.Seed = int32(int(uint32(psDD.Seed)) + int(uint32(int32(func() int {
				if 10 == 1 {
					return (int(psSS.Q_Q10) >> 1) + (int(psSS.Q_Q10) & 1)
				}
				return ((int(psSS.Q_Q10) >> (10 - 1)) + 1) >> 1
			}()))))
			psDD.RandState[*smpl_buf_idx] = psDD.Seed
			psDD.RD_Q10 = psSS.RD_Q10
		}
		delayedGain_Q10[*smpl_buf_idx] = Gain_Q10
	}
	for k = 0; k < nStatesDelayedDecision; k++ {
		psDD = &psDelDec[k]
		libc.MemCpy(unsafe.Pointer(&psDD.SLPC_Q14[0]), unsafe.Pointer(&psDD.SLPC_Q14[length]), int(MAX_LPC_ORDER*unsafe.Sizeof(int32(0))))
	}
}
func silk_nsq_del_dec_scale_states(psEncC *EncoderState, NSQ *NSQState, psDelDec []NSQ_del_dec_struct, x16 []int16, x_sc_Q10 []int32, sLTP []int16, sLTP_Q15 []int32, subfr int, nStatesDelayedDecision int, LTP_scale_Q14 int, Gains_Q16 [4]int32, pitchL [4]int, signal_type int, decisionDelay int) {
	var (
		i            int
		k            int
		lag          int
		gain_adj_Q16 int32
		inv_gain_Q31 int32
		inv_gain_Q26 int32
		psDD         *NSQ_del_dec_struct
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
			for i = NSQ.SLTP_buf_idx - lag - int(LTP_ORDER/2); i < NSQ.SLTP_buf_idx-decisionDelay; i++ {
				sLTP_Q15[i] = int32((int64(gain_adj_Q16) * int64(sLTP_Q15[i])) >> 16)
			}
		}
		for k = 0; k < nStatesDelayedDecision; k++ {
			psDD = &psDelDec[k]
			psDD.LF_AR_Q14 = int32((int64(gain_adj_Q16) * int64(psDD.LF_AR_Q14)) >> 16)
			psDD.Diff_Q14 = int32((int64(gain_adj_Q16) * int64(psDD.Diff_Q14)) >> 16)
			for i = 0; i < MAX_LPC_ORDER; i++ {
				psDD.SLPC_Q14[i] = int32((int64(gain_adj_Q16) * int64(psDD.SLPC_Q14[i])) >> 16)
			}
			for i = 0; i < MAX_SHAPE_LPC_ORDER; i++ {
				psDD.SAR2_Q14[i] = int32((int64(gain_adj_Q16) * int64(psDD.SAR2_Q14[i])) >> 16)
			}
			for i = 0; i < DECISION_DELAY; i++ {
				psDD.Pred_Q15[i] = int32((int64(gain_adj_Q16) * int64(psDD.Pred_Q15[i])) >> 16)
				psDD.Shape_Q14[i] = int32((int64(gain_adj_Q16) * int64(psDD.Shape_Q14[i])) >> 16)
			}
		}
		NSQ.Prev_gain_Q16 = Gains_Q16[subfr]
	}
}
