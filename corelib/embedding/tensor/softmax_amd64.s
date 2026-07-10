//go:build amd64

#include "textflag.h"

// func softmaxHMaxAVX2(scores *float32, n int) float32
// Horizontal max over n floats (n multiple of 8, n>=8).
// Frame: scores+0 n+8 ret+16 → 20
TEXT ·softmaxHMaxAVX2(SB), NOSPLIT, $0-20
	MOVQ scores+0(FP), SI
	MOVQ n+8(FP), CX
	VMOVUPS (SI), Y0
	ADDQ $32, SI
	SUBQ $8, CX
	TESTQ CX, CX
	JZ   sm_hmax_reduce

sm_hmax_loop:
	VMOVUPS (SI), Y1
	VMAXPS Y1, Y0, Y0
	ADDQ $32, SI
	SUBQ $8, CX
	JNZ  sm_hmax_loop

sm_hmax_reduce:
	VEXTRACTF128 $1, Y0, X1
	VMAXPS X1, X0, X0
	VSHUFPD $1, X0, X0, X1
	VMAXPS X1, X0, X0
	VMOVSHDUP X0, X1
	VMAXSS X1, X0, X0
	MOVSS X0, ret+16(FP)
	VZEROUPPER
	RET

// func softmaxHMaxDualAVX2(sc0, sc1 *float32, n int, out *[2]float32)
// Lockstep horizontal max over two equal-length rows (n multiple of 8).
// Frame: sc0+0 sc1+8 n+16 out+24 → 32
TEXT ·softmaxHMaxDualAVX2(SB), NOSPLIT, $0-32
	MOVQ sc0+0(FP), SI
	MOVQ sc1+8(FP), DI
	MOVQ n+16(FP), CX
	MOVQ out+24(FP), R11
	VMOVUPS (SI), Y0
	VMOVUPS (DI), Y1
	ADDQ $32, SI
	ADDQ $32, DI
	SUBQ $8, CX
	TESTQ CX, CX
	JZ   sm_hmaxd_reduce

sm_hmaxd_loop:
	VMOVUPS (SI), Y2
	VMAXPS Y2, Y0, Y0
	VMOVUPS (DI), Y3
	VMAXPS Y3, Y1, Y1
	ADDQ $32, SI
	ADDQ $32, DI
	SUBQ $8, CX
	JNZ  sm_hmaxd_loop

sm_hmaxd_reduce:
	// hmax Y0 → out[0]
	VEXTRACTF128 $1, Y0, X2
	VMAXPS X2, X0, X0
	VSHUFPD $1, X0, X0, X2
	VMAXPS X2, X0, X0
	VMOVSHDUP X0, X2
	VMAXSS X2, X0, X0
	MOVSS X0, (R11)
	// hmax Y1 → out[1]
	VEXTRACTF128 $1, Y1, X2
	VMAXPS X2, X1, X1
	VSHUFPD $1, X1, X1, X2
	VMAXPS X2, X1, X1
	VMOVSHDUP X1, X2
	VMAXSS X2, X1, X1
	MOVSS X1, 4(R11)
	VZEROUPPER
	RET

// func softmaxExpSumAVX2(scores *float32, n int, max, a, b, neg88 float32) float32
// scores[i] = fastExp(scores[i]-max) for n elements (multiple of 8); return sum.
// Dual-chunk (16 floats) when n>=16 for fewer branches.
// Frame: scores+0 n+8 max+16 a+20 b+24 neg88+28 ret+32 → 36
TEXT ·softmaxExpSumAVX2(SB), NOSPLIT, $0-36
	MOVQ scores+0(FP), SI
	MOVQ n+8(FP), CX

	MOVSS max+16(FP), X0
	VBROADCASTSS X0, Y15 // max
	MOVSS a+20(FP), X0
	VBROADCASTSS X0, Y13 // a
	MOVSS b+24(FP), X0
	VBROADCASTSS X0, Y12 // b
	MOVSS neg88+28(FP), X0
	VBROADCASTSS X0, Y11 // -88

	VXORPS Y14, Y14, Y14 // sum

	// Dual-chunk when n/16 > 0
	MOVQ CX, DX
	SHRQ $4, DX
	TESTQ DX, DX
	JZ   sm_exp_one

sm_exp_loop16:
	// chunk 0
	VMOVUPS (SI), Y0
	VSUBPS Y15, Y0, Y0
	VMAXPS Y11, Y0, Y0
	VMULPS Y13, Y0, Y0
	VADDPS Y12, Y0, Y0
	VCVTTPS2DQ Y0, Y0
	VMOVUPS Y0, (SI)
	VADDPS Y0, Y14, Y14
	// chunk 1
	VMOVUPS 32(SI), Y0
	VSUBPS Y15, Y0, Y0
	VMAXPS Y11, Y0, Y0
	VMULPS Y13, Y0, Y0
	VADDPS Y12, Y0, Y0
	VCVTTPS2DQ Y0, Y0
	VMOVUPS Y0, 32(SI)
	VADDPS Y0, Y14, Y14

	ADDQ $64, SI
	DECQ DX
	JNZ  sm_exp_loop16

	MOVQ n+8(FP), CX
	ANDQ $15, CX
	JZ   sm_exp_hsum

sm_exp_one:
	TESTQ CX, CX
	JZ   sm_exp_hsum

sm_exp_loop:
	VMOVUPS (SI), Y0
	VSUBPS Y15, Y0, Y0     // x - max
	VMAXPS Y11, Y0, Y0     // clamp >= -88
	// f = a*x + b  (Schraudolph)
	VMULPS Y13, Y0, Y0
	VADDPS Y12, Y0, Y0
	// i = int32(f); bit-cast to float = exp approx
	VCVTTPS2DQ Y0, Y0
	VMOVUPS Y0, (SI)
	// sum += exp (same bits as float)
	VADDPS Y0, Y14, Y14

	ADDQ $32, SI
	SUBQ $8, CX
	JNZ  sm_exp_loop

sm_exp_hsum:
	// hsum Y14 → X0
	VEXTRACTF128 $1, Y14, X0
	VADDPS X14, X0, X0
	VSHUFPD $1, X0, X0, X1
	VADDPS X0, X1, X0
	VMOVSHDUP X0, X1
	VADDSS X0, X1, X0
	MOVSS X0, ret+32(FP)
	VZEROUPPER
	RET

// func softmaxExpSumDualAVX2(sc0, sc1 *float32, n int, max0, max1, a, b, neg88 float32, out *[2]float32)
// Two independent score rows, lockstep exp+sum (attention nQ batch).
// out[0]=sum0, out[1]=sum1 (pointer avoids multi-float return packing issues).
// Frame: sc0+0 sc1+8 n+16 max0+24 max1+28 a+32 b+36 neg88+40 out+48 → 56
TEXT ·softmaxExpSumDualAVX2(SB), NOSPLIT, $0-56
	MOVQ sc0+0(FP), SI
	MOVQ sc1+8(FP), DI
	MOVQ n+16(FP), CX
	MOVQ out+48(FP), R11

	MOVSS max0+24(FP), X0
	VBROADCASTSS X0, Y15
	MOVSS max1+28(FP), X0
	VBROADCASTSS X0, Y14
	MOVSS a+32(FP), X0
	VBROADCASTSS X0, Y13
	MOVSS b+36(FP), X0
	VBROADCASTSS X0, Y12
	MOVSS neg88+40(FP), X0
	VBROADCASTSS X0, Y11

	VXORPS Y8, Y8, Y8   // sum0
	VXORPS Y9, Y9, Y9   // sum1

	MOVQ CX, DX
	SHRQ $3, DX         // n/8
	TESTQ DX, DX
	JZ   smd_hsum

smd_loop:
	// row 0
	VMOVUPS (SI), Y0
	VSUBPS Y15, Y0, Y0
	VMAXPS Y11, Y0, Y0
	VMULPS Y13, Y0, Y0
	VADDPS Y12, Y0, Y0
	VCVTTPS2DQ Y0, Y0
	VMOVUPS Y0, (SI)
	VADDPS Y0, Y8, Y8
	// row 1
	VMOVUPS (DI), Y1
	VSUBPS Y14, Y1, Y1
	VMAXPS Y11, Y1, Y1
	VMULPS Y13, Y1, Y1
	VADDPS Y12, Y1, Y1
	VCVTTPS2DQ Y1, Y1
	VMOVUPS Y1, (DI)
	VADDPS Y1, Y9, Y9

	ADDQ $32, SI
	ADDQ $32, DI
	DECQ DX
	JNZ  smd_loop

smd_hsum:
	// hsum Y8 → out[0]
	VEXTRACTF128 $1, Y8, X0
	VADDPS X8, X0, X0
	VSHUFPD $1, X0, X0, X1
	VADDPS X0, X1, X0
	VMOVSHDUP X0, X1
	VADDSS X0, X1, X0
	MOVSS X0, (R11)
	// hsum Y9 → out[1]
	VEXTRACTF128 $1, Y9, X0
	VADDPS X9, X0, X0
	VSHUFPD $1, X0, X0, X1
	VADDPS X0, X1, X0
	VMOVSHDUP X0, X1
	VADDSS X0, X1, X0
	MOVSS X0, 4(R11)
	VZEROUPPER
	RET
