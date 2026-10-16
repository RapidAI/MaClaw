//go:build arm64

#include "textflag.h"

// Horizontal reduction note: the Go arm64 assembler only exposes VFMLA/VFMLS
// as float vector ALU ops (VADD/VADDP/VADDV with .S4 are integer ops), so
// accumulators are reduced through a stack scratch with scalar FADDS.

// func dotNEON(a, b []float32) float32
// Computes the dot product sum(a[i]*b[i]) using NEON FMA with dual
// accumulators. Handles any length: 8-wide main loop, one 4-wide block,
// then a scalar tail. NEON counterpart of the amd64 AVX2/FMA dot kernels.
TEXT ·dotNEON(SB), NOSPLIT, $16-52
	MOVD a_base+0(FP), R0
	MOVD b_base+24(FP), R1
	MOVD a_len+8(FP), R2

	VEOR V0.B16, V0.B16, V0.B16 // accumulator A
	VEOR V1.B16, V1.B16, V1.B16 // accumulator B

	MOVD R2, R3
	LSR  $3, R3, R3
	CBZ  R3, dot_norem8

dot_loop8:
	VLD1.P 16(R0), [V2.S4]
	VLD1.P 16(R1), [V3.S4]
	VFMLA V3.S4, V2.S4, V0.S4
	VLD1.P 16(R0), [V4.S4]
	VLD1.P 16(R1), [V5.S4]
	VFMLA V5.S4, V4.S4, V1.S4
	SUB   $1, R3, R3
	CBNZ  R3, dot_loop8

dot_norem8:
	MOVD R2, R3
	AND  $4, R3, R3
	CBZ  R3, dot_scalars
	VLD1.P 16(R0), [V2.S4]
	VLD1.P 16(R1), [V3.S4]
	VFMLA V3.S4, V2.S4, V0.S4

dot_scalars:
	AND   $3, R2, R3
	FMOVS $0, F6
	CBZ   R3, dot_reduce

dot_scalar_loop:
	FMOVS (R0), F2
	FMOVS (R1), F3
	FMULS F2, F3, F4
	FADDS F4, F6, F6
	ADD   $4, R0, R0
	ADD   $4, R1, R1
	SUB   $1, R3, R3
	CBNZ  R3, dot_scalar_loop

dot_reduce:
	VMOV  V0.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F2
	VMOV  V0.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V0.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V0.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	FADDS F6, F2, F2
	FMOVS F2, ret+48(FP)
	RET

// func dot3NEON(a0, a1, a2, w0, w1, w2 []float32) float32
// Computes sum(a0*w0 + a1*w1 + a2*w2). All slices must have the same length.
// Operator-fused counterpart of the amd64 dot3AVX2/dot3FMA kernels: the three
// kernel taps accumulate into two independent NEON accumulators, hiding FMA
// latency while cutting loop overhead versus three separate dot products.
TEXT ·dot3NEON(SB), NOSPLIT, $16-148
	MOVD a0_base+0(FP), R0
	MOVD a1_base+24(FP), R1
	MOVD a2_base+48(FP), R2
	MOVD w0_base+72(FP), R6
	MOVD w1_base+96(FP), R7
	MOVD w2_base+120(FP), R8
	MOVD a0_len+8(FP), R3

	VEOR V0.B16, V0.B16, V0.B16 // accumulator A (taps 0 and 2)
	VEOR V1.B16, V1.B16, V1.B16 // accumulator B (tap 1)

	MOVD R3, R4
	LSR  $2, R4, R4
	CBZ  R4, dot3_scalars

dot3_loop4:
	VLD1.P 16(R0), [V3.S4]
	VLD1.P 16(R6), [V4.S4]
	VFMLA  V4.S4, V3.S4, V0.S4
	VLD1.P 16(R1), [V5.S4]
	VLD1.P 16(R7), [V6.S4]
	VFMLA  V6.S4, V5.S4, V1.S4
	VLD1.P 16(R2), [V7.S4]
	VLD1.P 16(R8), [V8.S4]
	VFMLA  V8.S4, V7.S4, V0.S4
	SUB    $1, R4, R4
	CBNZ   R4, dot3_loop4

dot3_scalars:
	AND   $3, R3, R4
	FMOVS $0, F10
	CBZ   R4, dot3_reduce

dot3_scalar_loop:
	FMOVS (R0), F2
	FMOVS (R6), F3
	FMULS F2, F3, F4
	FADDS F4, F10, F10
	FMOVS (R1), F2
	FMOVS (R7), F3
	FMULS F2, F3, F4
	FADDS F4, F10, F10
	FMOVS (R2), F2
	FMOVS (R8), F3
	FMULS F2, F3, F4
	FADDS F4, F10, F10
	ADD   $4, R0, R0
	ADD   $4, R1, R1
	ADD   $4, R2, R2
	ADD   $4, R6, R6
	ADD   $4, R7, R7
	ADD   $4, R8, R8
	SUB   $1, R4, R4
	CBNZ  R4, dot3_scalar_loop

dot3_reduce:
	VMOV  V0.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F2
	VMOV  V0.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V0.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V0.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	FADDS F10, F2, F2
	FMOVS F2, ret+144(FP)
	RET

// func dotStridedNEON(x []float32, xStride int, w []float32, wStride int, taps, n int) float32
// Computes sum over t in [0,taps) of dot(x[t*xStride : t*xStride+n],
// w[t*wStride : t*wStride+n]). Strides are in float32 elements and may be
// negative. NEON counterpart of dotStridedFMA: all taps accumulate in one
// pass with dual accumulators.
TEXT ·dotStridedNEON(SB), NOSPLIT, $16-84
	MOVD x_base+0(FP), R0
	MOVD xStride+24(FP), R4
	MOVD w_base+32(FP), R2
	MOVD wStride+56(FP), R5
	MOVD taps+64(FP), R6
	MOVD n+72(FP), R7
	// The vector loads and scalar loop already advance the pointers by n
	// elements per tap, so only the excess (stride - n) is added per tap.
	SUB  R7, R4, R4
	SUB  R7, R5, R5
	LSL  $2, R4, R4
	LSL  $2, R5, R5

	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	FMOVS $0, F6

	CBZ R6, ds_reduce

ds_tap:
	MOVD R7, R8
	LSR  $3, R8, R8
	CBZ  R8, ds_quad
ds_loop8:
	VLD1.P 16(R0), [V2.S4]
	VLD1.P 16(R2), [V3.S4]
	VFMLA  V3.S4, V2.S4, V0.S4
	VLD1.P 16(R0), [V4.S4]
	VLD1.P 16(R2), [V5.S4]
	VFMLA  V5.S4, V4.S4, V1.S4
	SUB    $1, R8, R8
	CBNZ   R8, ds_loop8
ds_quad:
	MOVD R7, R8
	AND  $4, R8, R8
	CBZ  R8, ds_scalars
	VLD1.P 16(R0), [V2.S4]
	VLD1.P 16(R2), [V3.S4]
	VFMLA  V3.S4, V2.S4, V0.S4
ds_scalars:
	MOVD R7, R8
	AND  $3, R8, R8
	CBZ  R8, ds_next
ds_scalar_loop:
	FMOVS (R0), F2
	FMOVS (R2), F3
	FMULS F2, F3, F4
	FADDS F4, F6, F6
	ADD   $4, R0, R0
	ADD   $4, R2, R2
	SUB   $1, R8, R8
	CBNZ  R8, ds_scalar_loop
ds_next:
	ADD  R4, R0, R0
	ADD  R5, R2, R2
	SUB  $1, R6, R6
	CBNZ R6, ds_tap

ds_reduce:
	VMOV  V0.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F2
	VMOV  V0.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V0.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V0.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	FADDS F6, F2, F2
	FMOVS F2, ret+80(FP)
	RET

// func dotStridedNEON2(x []float32, xStride int, w []float32, wStride int, taps, n int) (r0, r1 float32)
// Like dotStridedNEON but evaluates two outputs at once: output 0 starts at
// x[0], output 1 starts at x[n] (the next stride-1 output position). Weight
// chunks are loaded once per chunk and shared by both FMA chains. NEON
// counterpart of dotStridedFMA2.
TEXT ·dotStridedNEON2(SB), NOSPLIT, $16-88
	MOVD x_base+0(FP), R0
	MOVD xStride+24(FP), R4
	MOVD w_base+32(FP), R2
	MOVD wStride+56(FP), R5
	MOVD taps+64(FP), R6
	MOVD n+72(FP), R7
	// The vector loads and scalar loop already advance the pointers by n
	// elements per tap, so only the excess (stride - n) is added per tap.
	SUB  R7, R4, R4
	SUB  R7, R5, R5
	LSL  $2, R4, R4
	LSL  $2, R5, R5
	MOVD R7, R10
	LSL  $2, R10, R10 // byte offset of output 1 relative to output 0

	VEOR V0.B16, V0.B16, V0.B16 // output 0 accumulator A
	VEOR V1.B16, V1.B16, V1.B16 // output 0 accumulator B
	VEOR V8.B16, V8.B16, V8.B16 // output 1 accumulator A
	VEOR V9.B16, V9.B16, V9.B16 // output 1 accumulator B
	FMOVS $0, F12                 // output 0 scalar tail
	FMOVS $0, F13                // output 1 scalar tail

	CBZ R6, ds2_reduce

ds2_tap:
	ADD  R10, R0, R9 // output 1 base for this tap
	MOVD R7, R11
	LSR  $3, R11, R11
	CBZ  R11, ds2_quad
ds2_loop8:
	VLD1.P 16(R2), [V2.S4] // w, reused for both outputs
	VLD1.P 16(R0), [V3.S4]
	VLD1.P 16(R9), [V4.S4]
	VFMLA  V2.S4, V3.S4, V0.S4
	VFMLA  V2.S4, V4.S4, V8.S4
	VLD1.P 16(R2), [V5.S4]
	VLD1.P 16(R0), [V6.S4]
	VLD1.P 16(R9), [V7.S4]
	VFMLA  V5.S4, V6.S4, V1.S4
	VFMLA  V5.S4, V7.S4, V9.S4
	SUB    $1, R11, R11
	CBNZ   R11, ds2_loop8
ds2_quad:
	MOVD R7, R11
	AND  $4, R11, R11
	CBZ  R11, ds2_scalars
	VLD1.P 16(R2), [V2.S4]
	VLD1.P 16(R0), [V3.S4]
	VLD1.P 16(R9), [V4.S4]
	VFMLA  V2.S4, V3.S4, V0.S4
	VFMLA  V2.S4, V4.S4, V8.S4
ds2_scalars:
	MOVD R7, R11
	AND  $3, R11, R11
	CBZ  R11, ds2_next
ds2_scalar_loop:
	FMOVS (R2), F2
	FMOVS (R0), F3
	FMULS F2, F3, F4
	FADDS F4, F12, F12
	FMOVS (R9), F3
	FMULS F2, F3, F4
	FADDS F4, F13, F13
	ADD   $4, R0, R0
	ADD   $4, R2, R2
	ADD   $4, R9, R9
	SUB   $1, R11, R11
	CBNZ  R11, ds2_scalar_loop
ds2_next:
	ADD  R4, R0, R0
	ADD  R5, R2, R2
	SUB  $1, R6, R6
	CBNZ R6, ds2_tap

ds2_reduce:
	// r0: reduce V0 and V1
	VMOV  V0.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F2
	VMOV  V0.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V0.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V0.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V1.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	FADDS F12, F2, F2
	FMOVS F2, r0+80(FP)

	// r1: reduce V8 and V9
	VMOV  V8.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F2
	VMOV  V8.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V8.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V8.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V9.S[0], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V9.S[1], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V9.S[2], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	VMOV  V9.S[3], R5
	MOVW  R5, tmp-4(SP)
	FMOVS tmp-4(SP), F3
	FADDS F3, F2, F2
	FADDS F13, F2, F2
	FMOVS F2, r1+84(FP)
	RET
