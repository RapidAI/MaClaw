package libopus

import (
	"github.com/RapidAI/CodeClaw/corelib/opus/internal/libc"
	"math"
	"unsafe"
)

const USE_CELT_FIR = 0

func silk_LPC_analysis_filter(out []int16, in []int16, B []int16, len_ int32, d int32, arch int) {
	var (
		j         int
		ix        int
		out32_Q12 int32
		out32     int32
		in_ptr    *int16
	)
	_ = arch
	for ix = int(d); ix < int(len_); ix++ {
		in_ptr = &in[ix-1]
		out32_Q12 = int32(int(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), unsafe.Sizeof(int16(0))*0)))) * int(int32(B[0])))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), -int(unsafe.Sizeof(int16(0))*1)))))*int(int32(B[1]))))))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), -int(unsafe.Sizeof(int16(0))*2)))))*int(int32(B[2]))))))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), -int(unsafe.Sizeof(int16(0))*3)))))*int(int32(B[3]))))))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), -int(unsafe.Sizeof(int16(0))*4)))))*int(int32(B[4]))))))
		out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), -int(unsafe.Sizeof(int16(0))*5)))))*int(int32(B[5]))))))
		for j = 6; j < int(d); j += 2 {
			out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), -int(unsafe.Sizeof(int16(0))*uintptr(j))))))*int(int32(B[j]))))))
			out32_Q12 = int32(int(uint32(out32_Q12)) + int(uint32(int32(int(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), unsafe.Sizeof(int16(0))*uintptr(-j-1)))))*int(int32(B[j+1]))))))
		}
		out32_Q12 = int32(int(uint32(int32(int(uint32(int32(*(*int16)(unsafe.Add(unsafe.Pointer(in_ptr), unsafe.Sizeof(int16(0))*1)))))<<12))) - int(uint32(out32_Q12)))
		if 12 == 1 {
			out32 = int32((int(out32_Q12) >> 1) + (int(out32_Q12) & 1))
		} else {
			out32 = int32(((int(out32_Q12) >> (12 - 1)) + 1) >> 1)
		}
		if int(out32) > silk_int16_MAX {
			out[ix] = silk_int16_MAX
		} else if int(out32) < int(math.MinInt16) {
			out[ix] = math.MinInt16
		} else {
			out[ix] = int16(out32)
		}
	}
	libc.MemSet(unsafe.Pointer(&out[0]), 0, int(uintptr(d)*unsafe.Sizeof(int16(0))))
}
