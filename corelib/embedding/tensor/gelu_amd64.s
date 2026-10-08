//go:build amd64

#include "textflag.h"

// geluMulAVX2 applies gelu_pytorch_tanh(gate)*up for len(gate) lanes,
// eight at a time. Len must be a multiple of 8; the Go caller does the tail
// with geluPyTorchTanhFast.
//
// Positive tanh is 1-2/(exp(2*min(|z|,9))+1), then exactly 1 when |z|>=9.
// That identity stays within one float32 ulp of math.Tanh on [-40,40], so
// the Cody-Waite rational is not needed here. exp is the Cephes polynomial
// from expF32 (trunc toward zero, separate mul/add, no FMA).
// Go operand order is destination last: VSUBPS a, b, dst means dst=b-a;
// VDIVPS a, b, dst means dst=b/a; VCMPPS $5, a, b, dst means dst=(b>=a).

GLOBL ·geluTab<>(SB), RODATA|NOPTR, $64
DATA ·geluTab<>+0(SB)/4, $0x3F4C422A  // c = float32(√(2/π))
DATA ·geluTab<>+4(SB)/4, $0x3D372713  // 0.044715
DATA ·geluTab<>+8(SB)/4, $0x3FB8AA3B  // log2(e)
DATA ·geluTab<>+12(SB)/4, $0x3F000000 // 0.5
DATA ·geluTab<>+16(SB)/4, $0x3F318000 // exp c1
DATA ·geluTab<>+20(SB)/4, $0xB95E8083 // exp c2
DATA ·geluTab<>+24(SB)/4, $0x39506967 // exp p0
DATA ·geluTab<>+28(SB)/4, $0x3AB743CE // exp p1
DATA ·geluTab<>+32(SB)/4, $0x3C088908 // exp p2
DATA ·geluTab<>+36(SB)/4, $0x3D2AA9C1 // exp p3
DATA ·geluTab<>+40(SB)/4, $0x3E2AAAAA // exp p4
DATA ·geluTab<>+44(SB)/4, $0x3F800000 // 1
DATA ·geluTab<>+48(SB)/4, $0x40000000 // 2
DATA ·geluTab<>+52(SB)/4, $0x41100000 // 9
DATA ·geluTab<>+56(SB)/4, $0x0000007F // exp bias 127
DATA ·geluTab<>+60(SB)/4, $0x7FFFFFFF // abs mask

// func geluMulAVX2(gate, up []float32)
TEXT ·geluMulAVX2(SB), NOSPLIT, $0-48
	MOVQ gate_base+0(FP), SI
	MOVQ gate_len+8(FP), CX
	MOVQ up_base+24(FP), DI
	SHRQ $3, CX
	TESTQ CX, CX
	JZ   gelu_done

	LEAQ ·geluTab<>(SB), R10
	VBROADCASTSS 52(R10), Y12 // 9
	VBROADCASTSS 12(R10), Y13 // 0.5
	VBROADCASTSS 44(R10), Y14 // 1
	VPBROADCASTD 60(R10), Y15 // abs mask

gelu_loop:
	VMOVUPS (SI), Y0              // v
	VBROADCASTSS 4(R10), Y1
	VMULPS Y0, Y1, Y1             // ((k*v)*v)*v
	VMULPS Y0, Y1, Y1
	VMULPS Y0, Y1, Y1
	VADDPS Y0, Y1, Y1
	VBROADCASTSS 0(R10), Y2
	VMULPS Y2, Y1, Y1             // inner
	VANDPS Y15, Y1, Y2            // z = abs(inner)
	VCMPPS $5, Y12, Y2, Y10       // z >= 9
	VMINPS Y12, Y2, Y2            // exp argument stays finite

	VADDPS Y2, Y2, Y3             // x = 2*z
	VBROADCASTSS 8(R10), Y4
	VMULPS Y4, Y3, Y4             // fx = x*log2e + 0.5
	VADDPS Y13, Y4, Y4
	VCVTTPS2DQ Y4, Y5
	VCVTDQ2PS Y5, Y4
	VBROADCASTSS 16(R10), Y7
	VMULPS Y4, Y7, Y7             // g = (x-n*c1) - n*c2
	VSUBPS Y7, Y3, Y3
	VBROADCASTSS 20(R10), Y7
	VMULPS Y4, Y7, Y7
	VSUBPS Y7, Y3, Y3
	VMULPS Y3, Y3, Y7             // zz
	VBROADCASTSS 24(R10), Y6      // ((((p0*g+p1)*...)*g+0.5)*zz + g + 1
	VMULPS Y3, Y6, Y6
	VBROADCASTSS 28(R10), Y9
	VADDPS Y9, Y6, Y6
	VMULPS Y3, Y6, Y6
	VBROADCASTSS 32(R10), Y9
	VADDPS Y9, Y6, Y6
	VMULPS Y3, Y6, Y6
	VBROADCASTSS 36(R10), Y9
	VADDPS Y9, Y6, Y6
	VMULPS Y3, Y6, Y6
	VBROADCASTSS 40(R10), Y9
	VADDPS Y9, Y6, Y6
	VMULPS Y3, Y6, Y6
	VADDPS Y13, Y6, Y6
	VMULPS Y7, Y6, Y6
	VADDPS Y3, Y6, Y6
	VADDPS Y14, Y6, Y6
	VPBROADCASTD 56(R10), Y9      // (n+127)<<23
	VPADDD Y9, Y5, Y5
	VPSLLD $23, Y5, Y5
	VMULPS Y5, Y6, Y6             // e
	VADDPS Y14, Y6, Y6            // 1 - 2/(e+1)
	VBROADCASTSS 48(R10), Y9
	VDIVPS Y6, Y9, Y9
	VSUBPS Y9, Y14, Y6
	VANDPS Y10, Y14, Y4           // |z|>=9 → 1
	VANDNPS Y6, Y10, Y6
	VORPS Y4, Y6, Y6

	VXORPS Y9, Y9, Y9
	VCMPPS $1, Y9, Y1, Y9         // inner < 0
	VXORPS Y5, Y5, Y5
	VSUBPS Y6, Y5, Y5
	VANDPS Y9, Y5, Y5
	VANDNPS Y6, Y9, Y6
	VORPS Y5, Y6, Y6

	VADDPS Y14, Y6, Y6            // ((0.5*v)*(1+y))*up
	VMULPS Y13, Y0, Y0
	VMULPS Y6, Y0, Y0
	VMOVUPS (DI), Y1
	VMULPS Y1, Y0, Y0
	VMOVUPS Y0, (SI)

	ADDQ $32, SI
	ADDQ $32, DI
	DECQ CX
	JNZ  gelu_loop

gelu_done:
	VZEROUPPER
	RET
