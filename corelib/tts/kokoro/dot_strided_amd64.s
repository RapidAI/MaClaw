//go:build amd64

#include "textflag.h"


// func dotStridedFMA(x []float32, xStride int, w []float32, wStride int, taps, n int) float32
//
// Computes sum over t in [0,taps) of dot(x[t*xStride : t*xStride+n],
// w[t*wStride : t*wStride+n]). Strides are in float32 elements and may be
// negative. Requires AVX2+FMA.
//
// Accumulates all taps in four independent YMM accumulators to keep the FMA
// pipeline saturated (a single accumulator would be latency-bound).
TEXT ·dotStridedFMA(SB), NOSPLIT, $0-84
	MOVQ x_base+0(FP), SI
	MOVQ xStride+24(FP), R12
	SHLQ $2, R12
	MOVQ w_base+32(FP), DI
	MOVQ wStride+56(FP), R13
	SHLQ $2, R13
	MOVQ taps+64(FP), R14
	MOVQ n+72(FP), CX

	VXORPS Y8, Y8, Y8
	VXORPS Y9, Y9, Y9
	VXORPS Y10, Y10, Y10
	VXORPS Y11, Y11, Y11
	VXORPS X7, X7, X7

	TESTQ R14, R14
	JZ     dsf_reduce
	TESTQ  CX, CX
	JZ     dsf_reduce

dsf_tap:
	XORL BX, BX
	MOVQ CX, AX
	SHRQ $5, AX
	JZ   dsf_tail8

dsf_chunk32:
	VMOVUPS      (SI)(BX*4), Y0
	VMOVUPS      32(SI)(BX*4), Y1
	VMOVUPS      64(SI)(BX*4), Y2
	VMOVUPS      96(SI)(BX*4), Y3
	VFMADD231PS  (DI)(BX*4), Y0, Y8
	VFMADD231PS  32(DI)(BX*4), Y1, Y9
	VFMADD231PS  64(DI)(BX*4), Y2, Y10
	VFMADD231PS  96(DI)(BX*4), Y3, Y11
	ADDQ         $32, BX
	DECQ         AX
	JNZ          dsf_chunk32

dsf_tail8:
	MOVQ CX, DX
	ANDQ $31, DX
	JZ   dsf_next_tap
dsf_tail8_loop:
	CMPQ DX, $8
	JL   dsf_scalar
	VMOVUPS     (SI)(BX*4), Y0
	VFMADD231PS (DI)(BX*4), Y0, Y8
	ADDQ        $8, BX
	SUBQ        $8, DX
	JMP         dsf_tail8_loop

dsf_scalar:
	TESTQ DX, DX
	JZ    dsf_next_tap
dsf_scalar_loop:
	VMOVSS      (SI)(BX*4), X0
	VFMADD231SS (DI)(BX*4), X0, X7
	ADDQ        $1, BX
	DECQ        DX
	JNZ         dsf_scalar_loop

dsf_next_tap:
	ADDQ R12, SI
	ADDQ R13, DI
	DECQ R14
	JNZ  dsf_tap

dsf_reduce:
	VADDPS       Y9, Y8, Y8
	VADDPS       Y11, Y10, Y10
	VADDPS       Y10, Y8, Y8
	VEXTRACTF128 $1, Y8, X0
	VADDPS       X0, X8, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       X7, X0, X0
	MOVSS        X0, ret+80(FP)
	VZEROUPPER
	RET

// func dotStridedFMA2(x []float32, xStride int, w []float32, wStride int, taps, n int) (r0, r1 float32)
//
// Like dotStridedFMA but evaluates two outputs at once: output 0 starts at
// x[0], output 1 starts at x[n] (the next stride-1 output position). Weight
// chunks are loaded once and used for both FMA chains, halving weight-load
// traffic. Requires AVX2+FMA.
TEXT ·dotStridedFMA2(SB), NOSPLIT, $0-88
	MOVQ x_base+0(FP), SI
	MOVQ xStride+24(FP), R12
	SHLQ $2, R12
	MOVQ w_base+32(FP), DI
	MOVQ wStride+56(FP), R13
	SHLQ $2, R13
	MOVQ taps+64(FP), R14
	MOVQ n+72(FP), CX
	MOVQ CX, R15
	SHLQ $2, R15 // byte offset of output 1 relative to output 0

	VXORPS Y8, Y8, Y8
	VXORPS Y9, Y9, Y9
	VXORPS Y10, Y10, Y10
	VXORPS Y11, Y11, Y11
	VXORPS Y12, Y12, Y12
	VXORPS Y13, Y13, Y13
	VXORPS Y14, Y14, Y14
	VXORPS Y15, Y15, Y15
	VXORPS X7, X7, X7
	VXORPS X6, X6, X6

	TESTQ R14, R14
	JZ     dsf2_reduce
	TESTQ  CX, CX
	JZ     dsf2_reduce

dsf2_tap:
	LEAQ (SI)(R15*1), R10 // output 1 base for this tap
	XORL BX, BX
	MOVQ CX, AX
	SHRQ $5, AX
	JZ   dsf2_tail8

dsf2_chunk32:
	VMOVUPS      (DI)(BX*4), Y0
	VMOVUPS      32(DI)(BX*4), Y1
	VMOVUPS      64(DI)(BX*4), Y2
	VMOVUPS      96(DI)(BX*4), Y3
	VFMADD231PS  (SI)(BX*4), Y0, Y8
	VFMADD231PS  32(SI)(BX*4), Y1, Y9
	VFMADD231PS  64(SI)(BX*4), Y2, Y10
	VFMADD231PS  96(SI)(BX*4), Y3, Y11
	VFMADD231PS  (R10)(BX*4), Y0, Y12
	VFMADD231PS  32(R10)(BX*4), Y1, Y13
	VFMADD231PS  64(R10)(BX*4), Y2, Y14
	VFMADD231PS  96(R10)(BX*4), Y3, Y15
	ADDQ         $32, BX
	DECQ         AX
	JNZ          dsf2_chunk32

dsf2_tail8:
	MOVQ CX, DX
	ANDQ $31, DX
	JZ   dsf2_next_tap
dsf2_tail8_loop:
	CMPQ DX, $8
	JL   dsf2_scalar
	VMOVUPS     (DI)(BX*4), Y0
	VFMADD231PS (SI)(BX*4), Y0, Y8
	VFMADD231PS (R10)(BX*4), Y0, Y12
	ADDQ        $8, BX
	SUBQ        $8, DX
	JMP         dsf2_tail8_loop

dsf2_scalar:
	TESTQ DX, DX
	JZ    dsf2_next_tap
dsf2_scalar_loop:
	VMOVSS      (DI)(BX*4), X0
	VFMADD231SS (SI)(BX*4), X0, X7
	VFMADD231SS (R10)(BX*4), X0, X6
	ADDQ        $1, BX
	DECQ        DX
	JNZ         dsf2_scalar_loop

dsf2_next_tap:
	ADDQ R12, SI
	ADDQ R13, DI
	DECQ R14
	JNZ  dsf2_tap

dsf2_reduce:
	VADDPS       Y9, Y8, Y8
	VADDPS       Y11, Y10, Y10
	VADDPS       Y10, Y8, Y8
	VEXTRACTF128 $1, Y8, X0
	VADDPS       X0, X8, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       X7, X0, X0
	MOVSS        X0, r0+80(FP)

	VADDPS       Y13, Y12, Y12
	VADDPS       Y15, Y14, Y14
	VADDPS       Y14, Y12, Y12
	VEXTRACTF128 $1, Y12, X0
	VADDPS       X0, X12, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       X6, X0, X0
	MOVSS        X0, r1+84(FP)
	VZEROUPPER
	RET

// func dotStridedFMA4x2(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int) (r0, r1, r2, r3, r4, r5, r6, r7 float32)
//
// Computes four output channels at two adjacent output positions (8 results):
//   r[oc*2+ot] = sum_t dot(x[(ot+t)*xStride : +n], w[oc*wOcStride + t*wStride : +n])
// for oc in [0,4), ot in [0,2). x rows are loaded once and shared across the
// four output channels, and weight rows are shared across the two output
// positions, so per 8-wide chunk the kernel issues 6 loads for 8 FMAs.
// Requires n >= 8 and AVX2+FMA.
TEXT ·dotStridedFMA4x2(SB), NOSPLIT, $0-120
	MOVQ x_base+0(FP), SI
	MOVQ w_base+32(FP), DI
	MOVQ wStride+56(FP), R13
	SHLQ $2, R13
	MOVQ wOcStride+64(FP), R8
	SHLQ $2, R8
	MOVQ taps+72(FP), R14
	MOVQ n+80(FP), CX
	MOVQ CX, R15
	SHLQ $2, R15 // position 1 sits one row (n elements) after position 0
	MOVQ xStride+24(FP), R12
	SHLQ $2, R12 // tap stride for x

	LEAQ (SI)(R15*1), R11 // x base for output position 1

	// w pointers for output channels 1..3
	MOVQ wOcStride+64(FP), AX
	SHLQ $2, AX
	LEAQ (DI)(AX*1), R8
	LEAQ (R8)(AX*1), R9
	LEAQ (R9)(AX*1), R10

	VXORPS Y8, Y8, Y8
	VXORPS Y9, Y9, Y9
	VXORPS Y10, Y10, Y10
	VXORPS Y11, Y11, Y11
	VXORPS Y12, Y12, Y12
	VXORPS Y13, Y13, Y13
	VXORPS Y14, Y14, Y14
	VXORPS Y15, Y15, Y15


	// Hoist the per-tap invariants: AX = full 8-wide chunk count, DX = tail
	// lane count, R15 = &fmaMask8[tail]. Weights stay L1-resident across the
	// output loop, so the chunk loop uses them as FMA memory operands: 12
	// instructions per 8 FMAs keep the front end off the critical path.
	MOVQ CX, DX
	ANDQ $7, DX
	MOVQ DX, AX
	SHLQ $5, AX
	LEAQ ·fmaMask8(SB), R15
	ADDQ AX, R15
	MOVQ CX, AX
	SHRQ $3, AX

	TESTQ R14, R14
	JZ    dsf42_reduce
	TESTQ CX, CX
	JZ    dsf42_reduce

dsf42_tap:
	XORL BX, BX
	MOVQ CX, AX
	SHRQ $4, AX
	JZ   dsf42_maybe_single

	// Two 8-wide chunks per iteration: the second chunk reuses the weight
	// registers at +32 bytes and halves the x loads, cutting instructions
	// per FMA from 2.0 to 1.7. Accumulator spacing (8 FMAs between uses)
	// keeps the 4-cycle FMA latency covered.
dsf42_pair:
	VMOVUPS     (SI)(BX*4), Y0
	VMOVUPS     (R11)(BX*4), Y1
	VMOVUPS     32(SI)(BX*4), Y2
	VMOVUPS     32(R11)(BX*4), Y3
	VMOVUPS     (DI)(BX*4), Y4
	VMOVUPS     (R8)(BX*4), Y5
	VFMADD231PS Y4, Y0, Y8
	VFMADD231PS Y4, Y1, Y9
	VFMADD231PS Y5, Y0, Y10
	VFMADD231PS Y5, Y1, Y11
	VMOVUPS     (R9)(BX*4), Y6
	VMOVUPS     (R10)(BX*4), Y7
	VFMADD231PS Y6, Y0, Y12
	VFMADD231PS Y6, Y1, Y13
	VFMADD231PS Y7, Y0, Y14
	VFMADD231PS Y7, Y1, Y15
	VMOVUPS     32(DI)(BX*4), Y4
	VMOVUPS     32(R8)(BX*4), Y5
	VFMADD231PS Y4, Y2, Y8
	VFMADD231PS Y4, Y3, Y9
	VFMADD231PS Y5, Y2, Y10
	VFMADD231PS Y5, Y3, Y11
	VMOVUPS     32(R9)(BX*4), Y6
	VMOVUPS     32(R10)(BX*4), Y7
	VFMADD231PS Y6, Y2, Y12
	VFMADD231PS Y6, Y3, Y13
	VFMADD231PS Y7, Y2, Y14
	VFMADD231PS Y7, Y3, Y15
	ADDQ        $16, BX
	DECQ        AX
	JNZ         dsf42_pair
	TESTQ       $8, CX
	JZ          dsf42_tail
	JMP         dsf42_single

dsf42_maybe_single:
	TESTQ $8, CX
	JZ    dsf42_tail

dsf42_single:
	VMOVUPS     (SI)(BX*4), Y0
	VMOVUPS     (R11)(BX*4), Y1
	VMOVUPS     (DI)(BX*4), Y2
	VMOVUPS     (R8)(BX*4), Y3
	VFMADD231PS Y2, Y0, Y8
	VFMADD231PS Y2, Y1, Y9
	VFMADD231PS Y3, Y0, Y10
	VFMADD231PS Y3, Y1, Y11
	VMOVUPS     (R9)(BX*4), Y4
	VMOVUPS     (R10)(BX*4), Y5
	VFMADD231PS Y4, Y0, Y12
	VFMADD231PS Y4, Y1, Y13
	VFMADD231PS Y5, Y0, Y14
	VFMADD231PS Y5, Y1, Y15
dsf42_tail:
	TESTQ DX, DX
	JZ    dsf42_next_tap
	// Masked overlap: reload the last full 8-wide chunk (n >= 8) and zero
	// the lanes beyond the tail so they contribute nothing.
	MOVQ CX, BX
	SUBQ $8, BX
	VMOVUPS (SI)(BX*4), Y0
	VMOVUPS (R11)(BX*4), Y1
	VMOVUPS (R15), Y6
	VMULPS  Y6, Y0, Y0
	VMULPS  Y6, Y1, Y1
	VMOVUPS (DI)(BX*4), Y2
	VMOVUPS (R8)(BX*4), Y3
	VFMADD231PS Y2, Y0, Y8
	VFMADD231PS Y2, Y1, Y9
	VFMADD231PS Y3, Y0, Y10
	VFMADD231PS Y3, Y1, Y11
	VMOVUPS (R9)(BX*4), Y4
	VMOVUPS (R10)(BX*4), Y5
	VFMADD231PS Y4, Y0, Y12
	VFMADD231PS Y4, Y1, Y13
	VFMADD231PS Y5, Y0, Y14
	VFMADD231PS Y5, Y1, Y15

dsf42_next_tap:
	ADDQ R12, SI
	ADDQ R12, R11
	ADDQ R13, DI
	ADDQ R13, R8
	ADDQ R13, R9
	ADDQ R13, R10
	DECQ R14
	JNZ  dsf42_tap

dsf42_reduce:
	VEXTRACTF128 $1, Y8, X0
	VADDPS       X0, X8, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r0+88(FP)
	VEXTRACTF128 $1, Y9, X0
	VADDPS       X0, X9, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r1+92(FP)
	VEXTRACTF128 $1, Y10, X0
	VADDPS       X0, X10, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r2+96(FP)
	VEXTRACTF128 $1, Y11, X0
	VADDPS       X0, X11, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r3+100(FP)
	VEXTRACTF128 $1, Y12, X0
	VADDPS       X0, X12, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r4+104(FP)
	VEXTRACTF128 $1, Y13, X0
	VADDPS       X0, X13, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r5+108(FP)
	VEXTRACTF128 $1, Y14, X0
	VADDPS       X0, X14, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r6+112(FP)
	VEXTRACTF128 $1, Y15, X0
	VADDPS       X0, X15, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r7+116(FP)
	VZEROUPPER
	RET

// func dotStridedFMA4(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int) (r0, r1, r2, r3 float32)
//
// Computes one output position for four output channels, sharing each x row
// across all four weight rows:
//   r[oc] = sum_t dot(x[t*xStride : +n], w[oc*wOcStride + t*wStride : +n])
// Requires n >= 8 and AVX2+FMA.
TEXT ·dotStridedFMA4(SB), NOSPLIT, $0-104
	MOVQ x_base+0(FP), SI
	MOVQ w_base+32(FP), DI
	MOVQ wStride+56(FP), R13
	SHLQ $2, R13
	MOVQ wOcStride+64(FP), R8
	SHLQ $2, R8
	MOVQ taps+72(FP), R14
	MOVQ n+80(FP), CX
	MOVQ xStride+24(FP), R12
	SHLQ $2, R12

	// w pointers for output channels 1..3
	MOVQ wOcStride+64(FP), AX
	SHLQ $2, AX
	LEAQ (DI)(AX*1), R8
	LEAQ (R8)(AX*1), R9
	LEAQ (R9)(AX*1), R10

	VXORPS Y8, Y8, Y8
	VXORPS Y9, Y9, Y9
	VXORPS Y10, Y10, Y10
	VXORPS Y11, Y11, Y11

	TESTQ R14, R14
	JZ    dsf4_reduce
	TESTQ CX, CX
	JZ    dsf4_reduce

dsf4_tap:
	XORL BX, BX
	MOVQ CX, AX
	SHRQ $3, AX
	JZ   dsf4_tail

dsf4_chunk8:
	VMOVUPS     (SI)(BX*4), Y0
	VMOVUPS     (DI)(BX*4), Y1
	VMOVUPS     (R8)(BX*4), Y2
	VFMADD231PS Y1, Y0, Y8
	VFMADD231PS Y2, Y0, Y9
	VMOVUPS     (R9)(BX*4), Y3
	VMOVUPS     (R10)(BX*4), Y4
	VFMADD231PS Y3, Y0, Y10
	VFMADD231PS Y4, Y0, Y11
	ADDQ        $8, BX
	DECQ        AX
	JNZ         dsf4_chunk8

dsf4_tail:
	MOVQ CX, DX
	ANDQ $7, DX
	JZ   dsf4_next_tap
	LEAQ ·fmaMask8(SB), R15
	SHLQ $5, DX
	ADDQ DX, R15
	MOVQ CX, BX
	SUBQ $8, BX
	VMOVUPS (SI)(BX*4), Y0
	VMOVUPS (R15), Y5
	VMULPS  Y5, Y0, Y0
	VMOVUPS (DI)(BX*4), Y1
	VMOVUPS (R8)(BX*4), Y2
	VFMADD231PS Y1, Y0, Y8
	VFMADD231PS Y2, Y0, Y9
	VMOVUPS (R9)(BX*4), Y3
	VMOVUPS (R10)(BX*4), Y4
	VFMADD231PS Y3, Y0, Y10
	VFMADD231PS Y4, Y0, Y11

dsf4_next_tap:
	ADDQ R12, SI
	ADDQ R13, DI
	ADDQ R13, R8
	ADDQ R13, R9
	ADDQ R13, R10
	DECQ R14
	JNZ  dsf4_tap

dsf4_reduce:
	VEXTRACTF128 $1, Y8, X0
	VADDPS       X0, X8, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r0+88(FP)
	VEXTRACTF128 $1, Y9, X0
	VADDPS       X0, X9, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r1+92(FP)
	VEXTRACTF128 $1, Y10, X0
	VADDPS       X0, X10, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r2+96(FP)
	VEXTRACTF128 $1, Y11, X0
	VADDPS       X0, X11, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	MOVSS        X0, r3+100(FP)
	VZEROUPPER
	RET

// func transpose8x8F32AVX2(src []float32, srcStride int, dst []float32, dstStride int)
//
// Transposes one 8x8 float32 block: dst[j*dstStride+i] = src[i*srcStride+j]
// for i,j in [0,8). Strides are in float32 elements. Requires AVX2.
TEXT ·transpose8x8F32AVX2(SB), NOSPLIT, $0-64
	MOVQ src_base+0(FP), SI
	MOVQ srcStride+24(FP), R8
	SHLQ $2, R8
	MOVQ dst_base+32(FP), DI
	MOVQ dstStride+56(FP), R9
	SHLQ $2, R9

	// load 8 source rows
	VMOVUPS      (SI), Y0
	LEAQ         (SI)(R8*1), R10
	VMOVUPS      (R10), Y1
	LEAQ         (R10)(R8*1), R11
	VMOVUPS      (R11), Y2
	LEAQ         (R11)(R8*1), R12
	VMOVUPS      (R12), Y3
	LEAQ         (R12)(R8*1), R13
	VMOVUPS      (R13), Y4
	LEAQ         (R13)(R8*1), R14
	VMOVUPS      (R14), Y5
	LEAQ         (R14)(R8*1), R15
	VMOVUPS      (R15), Y6
	LEAQ         (R15)(R8*1), AX
	VMOVUPS      (AX), Y7

	// 8x8 transpose: unpack, shuffle, then rewire 128-bit lanes
	VUNPCKLPS Y1, Y0, Y8   // a0 b0 a1 b1 a4 b4 a5 b5
	VUNPCKHPS Y1, Y0, Y9   // a2 b2 a3 b3 a6 b6 a7 b7
	VUNPCKLPS Y3, Y2, Y10  // c0 d0 c1 d1 ...
	VUNPCKHPS Y3, Y2, Y11
	VUNPCKLPS Y5, Y4, Y12  // e0 f0 ...
	VUNPCKHPS Y5, Y4, Y13
	VUNPCKLPS Y7, Y6, Y14  // g0 h0 ...
	VUNPCKHPS Y7, Y6, Y15

	VSHUFPS   $0x44, Y10, Y8, Y0  // col0/4: a0 b0 c0 d0 a4 b4 c4 d4
	VSHUFPS   $0xee, Y10, Y8, Y1  // col1/5: a1 b1 c1 d1 a5 b5 c5 d5
	VSHUFPS   $0x44, Y11, Y9, Y2  // col2/6: a2 b2 c2 d2 a6 b6 c6 d6
	VSHUFPS   $0xee, Y11, Y9, Y3  // col3/7: a3 b3 c3 d3 a7 b7 c7 d7
	VSHUFPS   $0x44, Y14, Y12, Y4 // col0/4 of rows 4..7
	VSHUFPS   $0xee, Y14, Y12, Y5 // col1/5 of rows 4..7
	VSHUFPS   $0x44, Y15, Y13, Y6 // col2/6 of rows 4..7
	VSHUFPS   $0xee, Y15, Y13, Y7 // col3/7 of rows 4..7

	VPERM2F128 $0x20, Y4, Y0, Y8   // col0
	VPERM2F128 $0x20, Y5, Y1, Y9   // col1
	VPERM2F128 $0x20, Y6, Y2, Y10  // col2
	VPERM2F128 $0x20, Y7, Y3, Y11  // col3
	VPERM2F128 $0x31, Y4, Y0, Y12  // col4
	VPERM2F128 $0x31, Y5, Y1, Y13  // col5
	VPERM2F128 $0x31, Y6, Y2, Y14  // col6
	VPERM2F128 $0x31, Y7, Y3, Y15  // col7

	// store 8 destination rows (columns of the source block)
	VMOVUPS Y8, (DI)
	LEAQ    (DI)(R9*1), R10
	VMOVUPS Y9, (R10)
	LEAQ    (R10)(R9*1), R11
	VMOVUPS Y10, (R11)
	LEAQ    (R11)(R9*1), R12
	VMOVUPS Y11, (R12)
	LEAQ    (R12)(R9*1), R13
	VMOVUPS Y12, (R13)
	LEAQ    (R13)(R9*1), R14
	VMOVUPS Y13, (R14)
	LEAQ    (R14)(R9*1), R15
	VMOVUPS Y14, (R15)
	LEAQ    (R15)(R9*1), AX
	VMOVUPS Y15, (AX)
	VZEROUPPER
	RET

// func leakyReLUInplace32AVX2(x []float32, slope float32)
// x[i] = x[i] if x[i] >= 0 else slope*x[i]. Requires AVX2.
TEXT ·leakyReLUInplace32AVX2(SB), NOSPLIT, $0-28
	MOVQ         x_base+0(FP), SI
	MOVQ         x_len+8(FP), CX
	VBROADCASTSS slope+24(FP), Y1
	VXORPS       Y2, Y2, Y2

	MOVQ CX, AX
	SHRQ $3, AX
	JZ   lru_tail

lru_loop8:
	VMOVUPS (SI), Y0
	VMINPS  Y2, Y0, Y3
	VMAXPS  Y2, Y0, Y0
	VMULPS  Y1, Y3, Y3
	VADDPS  Y3, Y0, Y0
	VMOVUPS Y0, (SI)
	ADDQ    $32, SI
	DECQ    AX
	JNZ     lru_loop8

lru_tail:
	ANDQ $7, CX
	JZ   lru_done
lru_scalar:
	VMOVSS (SI), X0
	VMINSS X2, X0, X3
	VMAXSS X2, X0, X0
	VMULSS X1, X3, X3
	VADDSS X3, X0, X0
	VMOVSS X0, (SI)
	ADDQ   $4, SI
	DECQ   CX
	JNZ    lru_scalar

lru_done:
	VZEROUPPER
	RET

// func normScaleInto32AVX2(dst, x []float32, mean, scale, beta float32)
// dst[i] = (x[i]-mean)*scale + beta. Requires AVX2+FMA.
TEXT ·normScaleInto32AVX2(SB), NOSPLIT, $0-60
	MOVQ         dst_base+0(FP), SI
	MOVQ         x_base+24(FP), DI
	MOVQ         x_len+32(FP), CX
	VBROADCASTSS mean+48(FP), Y1
	VBROADCASTSS scale+52(FP), Y2
	VBROADCASTSS beta+56(FP), Y3

	MOVQ CX, AX
	SHRQ $3, AX
	JZ   ns_tail

ns_loop8:
	VMOVUPS       (DI), Y0
	VSUBPS        Y1, Y0, Y0
	VFMADD213PS   Y3, Y2, Y0
	VMOVUPS       Y0, (SI)
	ADDQ          $32, SI
	ADDQ          $32, DI
	DECQ          AX
	JNZ           ns_loop8

ns_tail:
	ANDQ $7, CX
	JZ   ns_done
ns_scalar:
	VMOVSS      (DI), X0
	VSUBSS      X1, X0, X0
	VFMADD213SS X3, X2, X0
	VMOVSS      X0, (SI)
	ADDQ        $4, SI
	ADDQ        $4, DI
	DECQ        CX
	JNZ         ns_scalar

ns_done:
	VZEROUPPER
	RET

// func axpyInplace32AVX2(dst, x []float32, a float32)
// dst[i] += a*x[i]. Requires AVX2+FMA.
TEXT ·axpyInplace32AVX2(SB), NOSPLIT, $0-52
	MOVQ         dst_base+0(FP), SI
	MOVQ         x_base+24(FP), DI
	MOVQ         x_len+32(FP), CX
	VBROADCASTSS a+48(FP), Y1

	MOVQ CX, AX
	SHRQ $3, AX
	JZ   ax_tail

ax_loop8:
	VMOVUPS     (SI), Y2
	VFMADD231PS (DI), Y1, Y2
	VMOVUPS     Y2, (SI)
	ADDQ        $32, SI
	ADDQ        $32, DI
	DECQ        AX
	JNZ         ax_loop8

ax_tail:
	ANDQ $7, CX
	JZ   ax_done
ax_scalar:
	VMOVSS      (SI), X2
	VFMADD231SS (DI), X1, X2
	VMOVSS      X2, (SI)
	ADDQ        $4, SI
	ADDQ        $4, DI
	DECQ        CX
	JNZ         ax_scalar

ax_done:
	VZEROUPPER
	RET

// func squareInplace32AVX2(x []float32)
// x[i] *= x[i]. Requires AVX2.
TEXT ·squareInplace32AVX2(SB), NOSPLIT, $0-24
	MOVQ x_base+0(FP), SI
	MOVQ x_len+8(FP), CX

	MOVQ CX, AX
	SHRQ $3, AX
	JZ   sq_tail

sq_loop8:
	VMOVUPS (SI), Y0
	VMULPS  Y0, Y0, Y0
	VMOVUPS Y0, (SI)
	ADDQ    $32, SI
	DECQ    AX
	JNZ     sq_loop8

sq_tail:
	ANDQ $7, CX
	JZ   sq_done
sq_scalar:
	VMOVSS (SI), X0
	VMULSS X0, X0, X0
	VMOVSS X0, (SI)
	ADDQ   $4, SI
	DECQ   CX
	JNZ    sq_scalar

sq_done:
	VZEROUPPER
	RET

// func normScaleScaledInto32AVX2(dst, scaled, x []float32, mean, scale, beta, a float32)
// dst[i] = (x[i]-mean)*scale + beta; scaled[i] = a*dst[i]. Requires AVX2+FMA.
TEXT ·normScaleScaledInto32AVX2(SB), NOSPLIT, $0-88
	MOVQ         dst_base+0(FP), SI
	MOVQ         scaled_base+24(FP), DI
	MOVQ         x_base+48(FP), R8
	MOVQ         x_len+56(FP), CX
	VBROADCASTSS mean+72(FP), Y1
	VBROADCASTSS scale+76(FP), Y2
	VBROADCASTSS beta+80(FP), Y3
	VBROADCASTSS a+84(FP), Y4

	MOVQ CX, AX
	SHRQ $3, AX
	JZ   nss_tail

nss_loop8:
	VMOVUPS     (R8), Y0
	VSUBPS      Y1, Y0, Y0
	VFMADD213PS Y3, Y2, Y0
	VMOVUPS     Y0, (SI)
	VMULPS      Y4, Y0, Y5
	VMOVUPS     Y5, (DI)
	ADDQ        $32, SI
	ADDQ        $32, DI
	ADDQ        $32, R8
	DECQ        AX
	JNZ         nss_loop8

nss_tail:
	ANDQ $7, CX
	JZ   nss_done
nss_scalar:
	VMOVSS      (R8), X0
	VSUBSS      X1, X0, X0
	VFMADD213SS X3, X2, X0
	VMOVSS      X0, (SI)
	VMULSS      X4, X0, X5
	VMOVSS      X5, (DI)
	ADDQ        $4, SI
	ADDQ        $4, DI
	ADDQ        $4, R8
	DECQ        CX
	JNZ         nss_scalar

nss_done:
	VZEROUPPER
	RET

// func dotStridedConv4x2G(x []float32, xStride int, w []float32, wStride int, wOcStride int, taps, n int, xPosStride int, out []float32, outOcStride int, outPosStride int, bias []float32, res []float32)
//
// Generalized fused conv epilogue: evaluates two adjacent output positions
// for four output channels and stores the results:
//
//	out[oc*outOcStride + p*outPosStride] =
//	    bias[oc] + (res[oc*outOcStride + p*outPosStride] if res nonempty)
//	    + sum_t dot(x[t*xStride + p*xPosStride : +n],
//	                w[oc*wOcStride + t*wStride : +n])
//
// for oc in [0,4), p in [0,2). bias must be non-empty (pass a zero slice when
// the conv has no bias); res may be empty. Requires n >= 8 and AVX2+FMA.
TEXT ·dotStridedConv4x2G(SB), NOSPLIT, $0-184
	MOVQ x_base+0(FP), SI
	MOVQ w_base+32(FP), DI
	MOVQ wStride+56(FP), R13
	SHLQ $2, R13
	MOVQ wOcStride+64(FP), R8
	SHLQ $2, R8
	MOVQ taps+72(FP), R14
	MOVQ n+80(FP), CX
	MOVQ xPosStride+88(FP), R11
	SHLQ $2, R11
	ADDQ SI, R11 // x base for output position 1
	MOVQ xStride+24(FP), R12
	SHLQ $2, R12

	// w pointers for output channels 1..3
	MOVQ wOcStride+64(FP), AX
	SHLQ $2, AX
	LEAQ (DI)(AX*1), R8
	LEAQ (R8)(AX*1), R9
	LEAQ (R9)(AX*1), R10

	VXORPS Y8, Y8, Y8
	VXORPS Y9, Y9, Y9
	VXORPS Y10, Y10, Y10
	VXORPS Y11, Y11, Y11
	VXORPS Y12, Y12, Y12
	VXORPS Y13, Y13, Y13
	VXORPS Y14, Y14, Y14
	VXORPS Y15, Y15, Y15

	MOVQ CX, DX
	ANDQ $7, DX
	MOVQ DX, AX
	SHLQ $5, AX
	LEAQ ·fmaMask8(SB), R15
	ADDQ AX, R15

	TESTQ R14, R14
	JZ    dsg_reduce
	TESTQ CX, CX
	JZ    dsg_reduce

dsg_tap:
	XORL BX, BX
	MOVQ CX, AX
	SHRQ $4, AX
	JZ   dsg_maybe_single

dsg_pair:
	VMOVUPS     (SI)(BX*4), Y0
	VMOVUPS     (R11)(BX*4), Y1
	VMOVUPS     32(SI)(BX*4), Y2
	VMOVUPS     32(R11)(BX*4), Y3
	VMOVUPS     (DI)(BX*4), Y4
	VMOVUPS     (R8)(BX*4), Y5
	VFMADD231PS Y4, Y0, Y8
	VFMADD231PS Y4, Y1, Y9
	VFMADD231PS Y5, Y0, Y10
	VFMADD231PS Y5, Y1, Y11
	VMOVUPS     (R9)(BX*4), Y6
	VMOVUPS     (R10)(BX*4), Y7
	VFMADD231PS Y6, Y0, Y12
	VFMADD231PS Y6, Y1, Y13
	VFMADD231PS Y7, Y0, Y14
	VFMADD231PS Y7, Y1, Y15
	VMOVUPS     32(DI)(BX*4), Y4
	VMOVUPS     32(R8)(BX*4), Y5
	VFMADD231PS Y4, Y2, Y8
	VFMADD231PS Y4, Y3, Y9
	VFMADD231PS Y5, Y2, Y10
	VFMADD231PS Y5, Y3, Y11
	VMOVUPS     32(R9)(BX*4), Y6
	VMOVUPS     32(R10)(BX*4), Y7
	VFMADD231PS Y6, Y2, Y12
	VFMADD231PS Y6, Y3, Y13
	VFMADD231PS Y7, Y2, Y14
	VFMADD231PS Y7, Y3, Y15
	ADDQ        $16, BX
	DECQ        AX
	JNZ         dsg_pair
	TESTQ       $8, CX
	JZ          dsg_tail
	JMP         dsg_single

dsg_maybe_single:
	TESTQ $8, CX
	JZ    dsg_tail

dsg_single:
	VMOVUPS     (SI)(BX*4), Y0
	VMOVUPS     (R11)(BX*4), Y1
	VMOVUPS     (DI)(BX*4), Y2
	VMOVUPS     (R8)(BX*4), Y3
	VFMADD231PS Y2, Y0, Y8
	VFMADD231PS Y2, Y1, Y9
	VFMADD231PS Y3, Y0, Y10
	VFMADD231PS Y3, Y1, Y11
	VMOVUPS     (R9)(BX*4), Y4
	VMOVUPS     (R10)(BX*4), Y5
	VFMADD231PS Y4, Y0, Y12
	VFMADD231PS Y4, Y1, Y13
	VFMADD231PS Y5, Y0, Y14
	VFMADD231PS Y5, Y1, Y15
dsg_tail:
	TESTQ DX, DX
	JZ    dsg_next_tap
	MOVQ  CX, BX
	SUBQ  $8, BX
	VMOVUPS (SI)(BX*4), Y0
	VMOVUPS (R11)(BX*4), Y1
	VMOVUPS (R15), Y6
	VMULPS  Y6, Y0, Y0
	VMULPS  Y6, Y1, Y1
	VMOVUPS (DI)(BX*4), Y2
	VMOVUPS (R8)(BX*4), Y3
	VFMADD231PS Y2, Y0, Y8
	VFMADD231PS Y3, Y0, Y10
	VFMADD231PS Y2, Y1, Y9
	VFMADD231PS Y3, Y1, Y11
	VMOVUPS (R9)(BX*4), Y4
	VMOVUPS (R10)(BX*4), Y5
	VFMADD231PS Y4, Y0, Y12
	VFMADD231PS Y5, Y0, Y14
	VFMADD231PS Y4, Y1, Y13
	VFMADD231PS Y5, Y1, Y15

dsg_next_tap:
	ADDQ R12, SI
	ADDQ R12, R11
	ADDQ R13, DI
	ADDQ R13, R8
	ADDQ R13, R9
	ADDQ R13, R10
	DECQ R14
	JNZ  dsg_tap

dsg_reduce:
	// Epilogue pointers: bias (BX), res (R15), out oc bases (SI, DI, R12,
	// R13), oc stride bytes (AX), position stride bytes (R14).
	MOVQ bias_base+136(FP), BX
	MOVQ res_base+160(FP), R15
	MOVQ outOcStride+120(FP), AX
	SHLQ $2, AX
	MOVQ outPosStride+128(FP), R14
	SHLQ $2, R14
	MOVQ out_base+96(FP), SI
	LEAQ (SI)(AX*1), DI
	LEAQ (DI)(AX*1), R12
	LEAQ (R12)(AX*1), R13
	MOVQ res_len+168(FP), R10

	// value 0: oc0 pos0
	VEXTRACTF128 $1, Y8, X0
	VADDPS       X0, X8, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       (BX), X0, X0
	TESTQ        R10, R10
	JZ           dsg_v1
	VADDSS       (R15), X0, X0
dsg_v1:
	MOVSS X0, (SI)
	VEXTRACTF128 $1, Y9, X0
	VADDPS       X0, X9, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       (BX), X0, X0
	TESTQ        R10, R10
	JZ           dsg_v2
	VADDSS       (R15)(R14*1), X0, X0
dsg_v2:
	MOVSS X0, (SI)(R14*1)
	LEAQ  (R15)(AX*1), R15
	VEXTRACTF128 $1, Y10, X0
	VADDPS       X0, X10, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       4(BX), X0, X0
	TESTQ        R10, R10
	JZ           dsg_v3
	VADDSS       (R15), X0, X0
dsg_v3:
	MOVSS X0, (DI)
	VEXTRACTF128 $1, Y11, X0
	VADDPS       X0, X11, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       4(BX), X0, X0
	TESTQ        R10, R10
	JZ           dsg_v4
	VADDSS       (R15)(R14*1), X0, X0
dsg_v4:
	MOVSS X0, (DI)(R14*1)
	LEAQ  (R15)(AX*1), R15
	VEXTRACTF128 $1, Y12, X0
	VADDPS       X0, X12, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       8(BX), X0, X0
	TESTQ        R10, R10
	JZ           dsg_v5
	VADDSS       (R15), X0, X0
dsg_v5:
	MOVSS X0, (R12)
	VEXTRACTF128 $1, Y13, X0
	VADDPS       X0, X13, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       8(BX), X0, X0
	TESTQ        R10, R10
	JZ           dsg_v6
	VADDSS       (R15)(R14*1), X0, X0
dsg_v6:
	MOVSS X0, (R12)(R14*1)
	LEAQ  (R15)(AX*1), R15
	VEXTRACTF128 $1, Y14, X0
	VADDPS       X0, X14, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       12(BX), X0, X0
	TESTQ        R10, R10
	JZ           dsg_v7
	VADDSS       (R15), X0, X0
dsg_v7:
	MOVSS X0, (R13)
	VEXTRACTF128 $1, Y15, X0
	VADDPS       X0, X15, X0
	VHADDPS      X0, X0, X0
	VHADDPS      X0, X0, X0
	VADDSS       12(BX), X0, X0
	TESTQ        R10, R10
	JZ           dsg_v8
	VADDSS       (R15)(R14*1), X0, X0
dsg_v8:
	MOVSS X0, (R13)(R14*1)
	VZEROUPPER
	RET
