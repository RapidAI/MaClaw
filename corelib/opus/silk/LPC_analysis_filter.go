package silk

import (
	"math"
	"unsafe"

	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
)

const USE_CELT_FIR = 0

func silk_LPC_analysis_filter(out []int16, in []int16, B []int16, len_ int32, d int32, arch int) {
	for ix := int(d); ix < int(len_); ix++ {
		ixi := ix - 1
		out32_Q12 := int32(int(in[ixi+0]) * int(B[0]))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(in[ixi-1])*int(B[1])))))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(in[ixi-2])*int(B[2])))))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(in[ixi-3])*int(B[3])))))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(in[ixi-4])*int(B[4])))))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(in[ixi-5])*int(B[5])))))
		for j := 6; j < int(d); j += 2 {
			out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(in[ixi-j])*int(B[j])))))
			out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(in[ixi-j-1])*int(B[j+1])))))
		}
		out32_Q12 = int32(int(uint32(int32(int(uint32(int32(in[ixi+1])))<<12))) - int(uint32(out32_Q12)))
		var out32 int32
		if 12 == 1 {
			out32 = int32((int(out32_Q12) >> 1) + (int(out32_Q12) & 1))
		} else {
			out32 = int32(((int(out32_Q12) >> (12 - 1)) + 1) >> 1)
		}
		if out32 > math.MaxInt16 {
			out[ix] = math.MaxInt16
		} else if out32 < math.MinInt16 {
			out[ix] = math.MinInt16
		} else {
			out[ix] = int16(out32)
		}
	}
	libc.MemSet(unsafe.Pointer(&out[0]), 0, int(uintptr(d)*unsafe.Sizeof(int16(0))))
}
