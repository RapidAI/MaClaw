package silk

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func EncodeIndices(psEncC *EncoderState, psRangeEnc *entcode.Encoder, FrameIndex int, encode_LBRR int, condCoding int) {
	var (
		i                        int
		k                        int
		typeOffset               int
		encode_absolute_lagIndex int
		delta_lagIndex           int
		ec_ix                    [16]int16
		pred_Q8                  [16]uint8
		psIndices                *SideInfoIndices
	)
	if encode_LBRR != 0 {
		psIndices = &psEncC.Indices_LBRR[FrameIndex]
	} else {
		psIndices = &psEncC.Indices
	}
	typeOffset = int(psIndices.SignalType)*2 + int(psIndices.QuantOffsetType)
	if encode_LBRR != 0 || typeOffset >= 2 {
		psRangeEnc.EncIcdf(typeOffset-2, silk_type_offset_VAD_iCDF[:], 8)
	} else {
		psRangeEnc.EncIcdf(typeOffset, silk_type_offset_no_VAD_iCDF[:], 8)
	}
	if condCoding == CODE_CONDITIONALLY {
		psRangeEnc.EncIcdf(int(psIndices.GainsIndices[0]), silk_delta_gain_iCDF[:], 8)
	} else {
		psRangeEnc.EncIcdf(int(psIndices.GainsIndices[0])>>3, silk_gain_iCDF[psIndices.SignalType][:], 8)
		psRangeEnc.EncIcdf(int(psIndices.GainsIndices[0])&7, silk_uniform8_iCDF[:], 8)
	}
	for i = 1; i < psEncC.Nb_subfr; i++ {
		psRangeEnc.EncIcdf(int(psIndices.GainsIndices[i]), silk_delta_gain_iCDF[:], 8)
	}
	psRangeEnc.EncIcdf(int(psIndices.NLSFIndices[0]), psEncC.PsNLSF_CB.CB1_iCDF[(int(psIndices.SignalType)>>1)*int(psEncC.PsNLSF_CB.NVectors):], 8)
	NLSF_unpack(ec_ix[:], pred_Q8[:], psEncC.PsNLSF_CB, int(psIndices.NLSFIndices[0]))
	for i = 0; i < int(psEncC.PsNLSF_CB.Order); i++ {
		if int(psIndices.NLSFIndices[i+1]) >= NLSF_QUANT_MAX_AMPLITUDE {
			psRangeEnc.EncIcdf(int(NLSF_QUANT_MAX_AMPLITUDE*2), psEncC.PsNLSF_CB.Ec_iCDF[ec_ix[i]:], 8)
			psRangeEnc.EncIcdf(int(psIndices.NLSFIndices[i+1])-NLSF_QUANT_MAX_AMPLITUDE, silk_NLSF_EXT_iCDF[:], 8)
		} else if int(psIndices.NLSFIndices[i+1]) <= -NLSF_QUANT_MAX_AMPLITUDE {
			psRangeEnc.EncIcdf(0, psEncC.PsNLSF_CB.Ec_iCDF[ec_ix[i]:], 8)
			psRangeEnc.EncIcdf(int(-psIndices.NLSFIndices[i+1])-NLSF_QUANT_MAX_AMPLITUDE, silk_NLSF_EXT_iCDF[:], 8)
		} else {
			psRangeEnc.EncIcdf(int(psIndices.NLSFIndices[i+1])+NLSF_QUANT_MAX_AMPLITUDE, psEncC.PsNLSF_CB.Ec_iCDF[ec_ix[i]:], 8)
		}
	}
	if psEncC.Nb_subfr == MAX_NB_SUBFR {
		psRangeEnc.EncIcdf(int(psIndices.NLSFInterpCoef_Q2), silk_NLSF_interpolation_factor_iCDF[:], 8)
	}
	if int(psIndices.SignalType) == TYPE_VOICED {
		encode_absolute_lagIndex = 1
		if condCoding == CODE_CONDITIONALLY && psEncC.Ec_prevSignalType == TYPE_VOICED {
			delta_lagIndex = int(psIndices.LagIndex) - int(psEncC.Ec_prevLagIndex)
			if delta_lagIndex < -8 || delta_lagIndex > 11 {
				delta_lagIndex = 0
			} else {
				delta_lagIndex = delta_lagIndex + 9
				encode_absolute_lagIndex = 0
			}
			psRangeEnc.EncIcdf(delta_lagIndex, silk_pitch_delta_iCDF[:], 8)
		}
		if encode_absolute_lagIndex != 0 {
			var (
				pitch_high_bits int32
				pitch_low_bits  int32
			)
			pitch_high_bits = int32(int(psIndices.LagIndex) / (psEncC.Fs_kHz >> 1))
			pitch_low_bits = int32(int(psIndices.LagIndex) - int(int32(int16(pitch_high_bits)))*int(int32(int16(psEncC.Fs_kHz>>1))))
			psRangeEnc.EncIcdf(int(pitch_high_bits), silk_pitch_lag_iCDF[:], 8)
			psRangeEnc.EncIcdf(int(pitch_low_bits), psEncC.Pitch_lag_low_bits_iCDF, 8)
		}
		psEncC.Ec_prevLagIndex = psIndices.LagIndex
		psRangeEnc.EncIcdf(int(psIndices.ContourIndex), psEncC.Pitch_contour_iCDF, 8)
		psRangeEnc.EncIcdf(int(psIndices.PERIndex), silk_LTP_per_index_iCDF[:], 8)
		for k = 0; k < psEncC.Nb_subfr; k++ {
			psRangeEnc.EncIcdf(int(psIndices.LTPIndex[k]), []byte(silk_LTP_gain_iCDF_ptrs[psIndices.PERIndex]), 8)
		}
		if condCoding == CODE_INDEPENDENTLY {
			psRangeEnc.EncIcdf(int(psIndices.LTP_scaleIndex), silk_LTPscale_iCDF[:], 8)
		}
	}
	psEncC.Ec_prevSignalType = int(psIndices.SignalType)
	psRangeEnc.EncIcdf(int(psIndices.Seed), silk_uniform4_iCDF[:], 8)
}
