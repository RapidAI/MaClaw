package silk

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func DecodeIndices(d *DecoderState, psRangeDec *entcode.Decoder, FrameIndex int, decode_LBRR bool, condCoding int) {
	var (
		Ix      int
		ec_ix   [16]int16
		pred_Q8 [16]uint8
	)
	if decode_LBRR || d.VAD_flags[FrameIndex] != 0 {
		Ix = psRangeDec.DecIcdf(silk_type_offset_VAD_iCDF[:], 8) + 2
	} else {
		Ix = psRangeDec.DecIcdf(silk_type_offset_no_VAD_iCDF[:], 8)
	}
	d.Indices.SignalType = int8(Ix >> 1)
	d.Indices.QuantOffsetType = int8(Ix & 1)
	if condCoding == CODE_CONDITIONALLY {
		d.Indices.GainsIndices[0] = int8(psRangeDec.DecIcdf(silk_delta_gain_iCDF[:], 8))
	} else {
		d.Indices.GainsIndices[0] = int8(int32(int(uint32(int32(psRangeDec.DecIcdf(silk_gain_iCDF[d.Indices.SignalType][:], 8)))) << 3))
		d.Indices.GainsIndices[0] += int8(psRangeDec.DecIcdf(silk_uniform8_iCDF[:], 8))
	}
	for i := 1; i < d.Nb_subfr; i++ {
		d.Indices.GainsIndices[i] = int8(psRangeDec.DecIcdf(silk_delta_gain_iCDF[:], 8))
	}
	d.Indices.NLSFIndices[0] = int8(psRangeDec.DecIcdf(d.PsNLSF_CB.CB1_iCDF[(int(d.Indices.SignalType)>>1)*int(d.PsNLSF_CB.NVectors):], 8))
	NLSF_unpack(ec_ix[:], pred_Q8[:], d.PsNLSF_CB, int(d.Indices.NLSFIndices[0]))
	for i := 0; i < int(d.PsNLSF_CB.Order); i++ {
		Ix = psRangeDec.DecIcdf(d.PsNLSF_CB.Ec_iCDF[ec_ix[i]:], 8)
		if Ix == 0 {
			Ix -= psRangeDec.DecIcdf(silk_NLSF_EXT_iCDF[:], 8)
		} else if Ix == int(NLSF_QUANT_MAX_AMPLITUDE*2) {
			Ix += psRangeDec.DecIcdf(silk_NLSF_EXT_iCDF[:], 8)
		}
		d.Indices.NLSFIndices[i+1] = int8(Ix - NLSF_QUANT_MAX_AMPLITUDE)
	}
	if d.Nb_subfr == MAX_NB_SUBFR {
		d.Indices.NLSFInterpCoef_Q2 = int8(psRangeDec.DecIcdf(silk_NLSF_interpolation_factor_iCDF[:], 8))
	} else {
		d.Indices.NLSFInterpCoef_Q2 = 4
	}
	if int(d.Indices.SignalType) == TYPE_VOICED {
		decode_absolute_lagIndex := true
		if condCoding == CODE_CONDITIONALLY && d.Ec_prevSignalType == TYPE_VOICED {
			delta_lagIndex := int(int16(psRangeDec.DecIcdf(silk_pitch_delta_iCDF[:], 8)))
			if delta_lagIndex > 0 {
				delta_lagIndex = delta_lagIndex - 9
				d.Indices.LagIndex = int16(int(d.Ec_prevLagIndex) + delta_lagIndex)
				decode_absolute_lagIndex = false
			}
		}
		if decode_absolute_lagIndex {
			d.Indices.LagIndex = int16(int(int16(psRangeDec.DecIcdf(silk_pitch_lag_iCDF[:], 8))) * (d.Fs_kHz >> 1))
			d.Indices.LagIndex += int16(psRangeDec.DecIcdf(d.Pitch_lag_low_bits_iCDF, 8))
		}
		d.Ec_prevLagIndex = d.Indices.LagIndex
		d.Indices.ContourIndex = int8(psRangeDec.DecIcdf(d.Pitch_contour_iCDF, 8))
		d.Indices.PERIndex = int8(psRangeDec.DecIcdf(silk_LTP_per_index_iCDF[:], 8))
		for k := 0; k < d.Nb_subfr; k++ {
			d.Indices.LTPIndex[k] = int8(psRangeDec.DecIcdf(silk_LTP_gain_iCDF_ptrs[d.Indices.PERIndex], 8))
		}
		if condCoding == CODE_INDEPENDENTLY {
			d.Indices.LTP_scaleIndex = int8(psRangeDec.DecIcdf(silk_LTPscale_iCDF[:], 8))
		} else {
			d.Indices.LTP_scaleIndex = 0
		}
	}
	d.Ec_prevSignalType = int(d.Indices.SignalType)
	d.Indices.Seed = int8(psRangeDec.DecIcdf(silk_uniform4_iCDF[:], 8))
}
