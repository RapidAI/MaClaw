package silk

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func EncodeSigns(psRangeEnc *entcode.Encoder, pulses []int8, length int, signalType int, quantOffsetType int, sum_pulses [20]int) {
	var icdf [2]uint8
	icdf[1] = 0
	q_ptr := pulses
	i := int(int32(int16(quantOffsetType+int(int32(int(uint32(int32(signalType)))<<1))))) * 7
	icdf_ptr := silk_sign_iCDF[i:]
	length = (length + int(SHELL_CODEC_FRAME_LENGTH/2)) >> LOG2_SHELL_CODEC_FRAME_LENGTH
	for i := 0; i < length; i++ {
		p := sum_pulses[i]
		if p > 0 {
			icdf[0] = icdf_ptr[func() int {
				if (p & 0x1F) < 6 {
					return p & 0x1F
				}
				return 6
			}()]
			for j := 0; j < SHELL_CODEC_FRAME_LENGTH; j++ {
				if q_ptr[j] != 0 {
					psRangeEnc.EncIcdf((int(q_ptr[j])>>15)+1, icdf[:], 8)
				}
			}
		}
		q_ptr = q_ptr[SHELL_CODEC_FRAME_LENGTH:]
	}
}
func decodeSigns(psRangeDec *entcode.Decoder, pulses []int16, length int, signalType int, quantOffsetType int, sum_pulses [20]int) {
	var (
		p    int
		icdf [2]uint8
	)
	icdf[1] = 0
	q_ptr := pulses
	i := int(int32(int16(quantOffsetType+int(int32(int(uint32(int32(signalType)))<<1))))) * 7
	icdf_ptr := silk_sign_iCDF[i:]
	length = (length + int(SHELL_CODEC_FRAME_LENGTH/2)) >> LOG2_SHELL_CODEC_FRAME_LENGTH
	for i := 0; i < length; i++ {
		p = sum_pulses[i]
		if p > 0 {
			icdf[0] = icdf_ptr[func() int {
				if (p & 0x1F) < 6 {
					return p & 0x1F
				}
				return 6
			}()]
			for j := 0; j < SHELL_CODEC_FRAME_LENGTH; j++ {
				if q_ptr[j] > 0 {
					q_ptr[j] *= int16(int(int32(int(uint32(int32(psRangeDec.DecIcdf(icdf[:], 8))))<<1)) - 1)
				}
			}
		}
		q_ptr = q_ptr[SHELL_CODEC_FRAME_LENGTH:]
	}
}
