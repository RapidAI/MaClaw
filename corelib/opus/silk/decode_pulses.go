package silk

import (
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"

	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func DecodePulses(psRangeDec *entcode.Decoder, pulses []int16, signalType int, quantOffsetType int, frame_length int) {
	var (
		sum_pulses [20]int
		nLshifts   [20]int
	)
	RateLevelIndex := psRangeDec.DecIcdf(silk_rate_levels_iCDF[signalType>>1][:], 8)
	iter := frame_length >> LOG2_SHELL_CODEC_FRAME_LENGTH
	if iter*SHELL_CODEC_FRAME_LENGTH < frame_length {
		iter++
	}
	cdf_ptr := silk_pulses_per_block_iCDF[RateLevelIndex][:]
	for i := 0; i < iter; i++ {
		nLshifts[i] = 0
		sum_pulses[i] = psRangeDec.DecIcdf(cdf_ptr, 8)
		for sum_pulses[i] == SILK_MAX_PULSES+1 {
			nLshifts[i]++
			ish := 0
			if nLshifts[i] == 10 {
				ish++
			}
			sum_pulses[i] = psRangeDec.DecIcdf(silk_pulses_per_block_iCDF[N_RATE_LEVELS-1][ish:], 8)
		}
	}
	for i := 0; i < iter; i++ {
		if sum_pulses[i] > 0 {
			shellDecoder(pulses[int(int16(i))*SHELL_CODEC_FRAME_LENGTH:], psRangeDec, sum_pulses[i])
		} else {
			libc.MemSet(unsafe.Pointer(&pulses[int(int16(i))*SHELL_CODEC_FRAME_LENGTH]), 0, int(SHELL_CODEC_FRAME_LENGTH*unsafe.Sizeof(int16(0))))
		}
	}
	for i := 0; i < iter; i++ {
		if nLshifts[i] > 0 {
			nLS := nLshifts[i]
			pulses_ptr := pulses[int(int16(i))*SHELL_CODEC_FRAME_LENGTH:]
			for k := 0; k < SHELL_CODEC_FRAME_LENGTH; k++ {
				abs_q := int(pulses_ptr[k])
				for j := 0; j < nLS; j++ {
					abs_q = abs_q << 1
					abs_q += psRangeDec.DecIcdf(silk_lsb_iCDF[:], 8)
				}
				pulses_ptr[k] = int16(abs_q)
			}
			sum_pulses[i] |= nLS << 5
		}
	}
	decodeSigns(psRangeDec, pulses, frame_length, signalType, quantOffsetType, sum_pulses)
}
