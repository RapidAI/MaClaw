package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"

	"github.com/RapidAI/CodeClaw/corelib/opus/entcode"
)

func combineAndCheck(pulses_comb []int, pulses_in []int, max_pulses int, len_ int) int {
	for k := 0; k < len_; k++ {
		sum := pulses_in[k*2] + pulses_in[k*2+1]
		if sum > max_pulses {
			return 1
		}
		pulses_comb[k] = sum
	}
	return 0
}

func EncodePulses(psRangeEnc *entcode.Encoder, signalType int, quantOffsetType int, pulses []int8, frame_length int) {
	var (
		RateLevelIndex int = 0
		pulses_comb    [8]int
	)
	iter := frame_length >> LOG2_SHELL_CODEC_FRAME_LENGTH
	if iter*SHELL_CODEC_FRAME_LENGTH < frame_length {
		iter++
		libc.MemSet(unsafe.Pointer(&pulses[frame_length]), 0, int(SHELL_CODEC_FRAME_LENGTH*unsafe.Sizeof(int8(0))))
	}
	abs_pulses := make([]int, iter*SHELL_CODEC_FRAME_LENGTH)
	for i := 0; i < iter*SHELL_CODEC_FRAME_LENGTH; i += 4 {
		if int(pulses[i+0]) > 0 {
			abs_pulses[i+0] = int(pulses[i+0])
		} else {
			abs_pulses[i+0] = -int(pulses[i+0])
		}
		if int(pulses[i+1]) > 0 {
			abs_pulses[i+1] = int(pulses[i+1])
		} else {
			abs_pulses[i+1] = -int(pulses[i+1])
		}
		if int(pulses[i+2]) > 0 {
			abs_pulses[i+2] = int(pulses[i+2])
		} else {
			abs_pulses[i+2] = -int(pulses[i+2])
		}
		if int(pulses[i+3]) > 0 {
			abs_pulses[i+3] = int(pulses[i+3])
		} else {
			abs_pulses[i+3] = -int(pulses[i+3])
		}
	}
	sum_pulses := make([]int, iter)
	nRshifts := make([]int, iter)
	abs_pulses_ptr := abs_pulses
	for i := 0; i < iter; i++ {
		nRshifts[i] = 0
		for {
			scale_down := combineAndCheck(pulses_comb[:], abs_pulses_ptr, int(silk_max_pulses_table[0]), 8)
			scale_down += combineAndCheck(pulses_comb[:], pulses_comb[:], int(silk_max_pulses_table[1]), 4)
			scale_down += combineAndCheck(pulses_comb[:], pulses_comb[:], int(silk_max_pulses_table[2]), 2)
			scale_down += combineAndCheck(sum_pulses[i:], pulses_comb[:], int(silk_max_pulses_table[3]), 1)
			if scale_down != 0 {
				nRshifts[i]++
				for k := 0; k < SHELL_CODEC_FRAME_LENGTH; k++ {
					abs_pulses_ptr[k] = abs_pulses_ptr[k] >> 1
				}
			} else {
				break
			}
		}
		abs_pulses_ptr = abs_pulses_ptr[SHELL_CODEC_FRAME_LENGTH:]
	}
	minSumBits_Q5 := math.MaxInt
	for k := 0; k < N_RATE_LEVELS-1; k++ {
		nBits_ptr := silk_pulses_per_block_BITS_Q5[k][:]
		sumBits_Q5 := int(silk_rate_levels_BITS_Q5[signalType>>1][k])
		for i := 0; i < iter; i++ {
			if nRshifts[i] > 0 {
				sumBits_Q5 += int(nBits_ptr[SILK_MAX_PULSES+1])
			} else {
				sumBits_Q5 += int(nBits_ptr[sum_pulses[i]])
			}
		}
		if sumBits_Q5 < minSumBits_Q5 {
			minSumBits_Q5 = sumBits_Q5
			RateLevelIndex = k
		}
	}
	psRangeEnc.EncIcdf(RateLevelIndex, silk_rate_levels_iCDF[signalType>>1][:], 8)
	cdf_ptr := silk_pulses_per_block_iCDF[RateLevelIndex][:]
	for i := 0; i < iter; i++ {
		if nRshifts[i] == 0 {
			psRangeEnc.EncIcdf(sum_pulses[i], cdf_ptr, 8)
		} else {
			psRangeEnc.EncIcdf(SILK_MAX_PULSES+1, cdf_ptr, 8)
			for k := 0; k < nRshifts[i]-1; k++ {
				psRangeEnc.EncIcdf(SILK_MAX_PULSES+1, silk_pulses_per_block_iCDF[N_RATE_LEVELS-1][:], 8)
			}
			psRangeEnc.EncIcdf(sum_pulses[i], silk_pulses_per_block_iCDF[N_RATE_LEVELS-1][:], 8)
		}
	}
	for i := 0; i < iter; i++ {
		if sum_pulses[i] > 0 {
			ShellEncoder(psRangeEnc, abs_pulses[i*SHELL_CODEC_FRAME_LENGTH:])
		}
	}
	for i := 0; i < iter; i++ {
		if nRshifts[i] > 0 {
			pulses_ptr := pulses[i*SHELL_CODEC_FRAME_LENGTH:]
			nLS := nRshifts[i] - 1
			for k := 0; k < SHELL_CODEC_FRAME_LENGTH; k++ {
				var abs_q int
				if pulses_ptr[k] > 0 {
					abs_q = int(pulses_ptr[k])
				} else {
					abs_q = -int(pulses_ptr[k])
				}
				for j := nLS; j > 0; j-- {
					bit := (abs_q >> j) & 1
					psRangeEnc.EncIcdf(bit, silk_lsb_iCDF[:], 8)
				}
				bit := abs_q & 1
				psRangeEnc.EncIcdf(bit, silk_lsb_iCDF[:], 8)
			}
		}
	}
	EncodeSigns(psRangeEnc, pulses, frame_length, signalType, quantOffsetType, [20]int(sum_pulses))
}
