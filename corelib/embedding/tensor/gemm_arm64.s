//go:build arm64

#include "textflag.h"

// ARM64 NEON WORD encodings:
// FMLA  Vd.4S, Vn.4S, Vm.4S  = 0x4E20CC00 | Rm<<16 | Rn<<5 | Rd
// FADDP Vd.4S, Vn.4S, Vm.4S  = 0x6E20D400 | Rm<<16 | Rn<<5 | Rd
// FADDP Sd, Vn.2S            = 0x7E30D800 | Rn<<5 | Rd

// func multiDot4NEON(out *[4]float32, a, b *float32, K int)
//
// out[i] = sum_k a[i*K + k] * b[k]  for i in 0..3
// a is contiguous [4][K], b is [K].
//
// Frame: out+0, a+8, b+16, K+24
TEXT ·multiDot4NEON(SB), NOSPLIT, $0-32
	MOVD  out+0(FP), R0
	MOVD  a+8(FP), R1
	MOVD  b+16(FP), R2
	MOVD  K+24(FP), R3

	// Row stride bytes = K * 4
	LSL   $2, R3, R4

	// Row bases: R5..R8
	MOVD  R1, R5
	ADD   R4, R5, R6
	ADD   R4, R6, R7
	ADD   R4, R7, R8

	// Zero accumulators V0..V3
	VEOR  V0.B16, V0.B16, V0.B16
	VEOR  V1.B16, V1.B16, V1.B16
	VEOR  V2.B16, V2.B16, V2.B16
	VEOR  V3.B16, V3.B16, V3.B16

	// Main loop: K/4 vectors of 4 floats
	LSR   $2, R3, R9
	CBZ   R9, md4_hsum

md4_loop:
	// V4 = B[k:k+4]
	VLD1.P 16(R2), [V4.S4]

	// row0
	VLD1.P 16(R5), [V5.S4]
	// FMLA V0.4S, V5.4S, V4.4S: Rd=0 Rn=5 Rm=4
	WORD  $0x4E24CCA0
	// row1
	VLD1.P 16(R6), [V5.S4]
	// FMLA V1.4S, V5.4S, V4.4S: Rd=1 Rn=5 Rm=4
	WORD  $0x4E24CCA1
	// row2
	VLD1.P 16(R7), [V5.S4]
	// FMLA V2.4S, V5.4S, V4.4S: Rd=2 Rn=5 Rm=4
	WORD  $0x4E24CCA2
	// row3
	VLD1.P 16(R8), [V5.S4]
	// FMLA V3.4S, V5.4S, V4.4S: Rd=3 Rn=5 Rm=4
	WORD  $0x4E24CCA3

	SUB   $1, R9, R9
	CBNZ  R9, md4_loop

md4_hsum:
	// Horizontal sum V0..V3 → out[0..3]
	// FADDP V0.4S, V0.4S, V0.4S
	WORD  $0x6E20D400
	// FADDP S0, V0.2S
	WORD  $0x7E30D800
	FMOVS F0, (R0)

	// FADDP V1.4S, V1.4S, V1.4S: Rm=1 Rn=1 Rd=1
	// = 0x6E20D400 | (1<<16) | (1<<5) | 1 = 0x6E21D421
	WORD  $0x6E21D421
	// FADDP S1, V1.2S: Rn=1 Rd=1 = 0x7E30D800 | (1<<5) | 1 = 0x7E30D821
	WORD  $0x7E30D821
	FMOVS F1, 4(R0)

	// FADDP V2.4S, V2.4S, V2.4S = 0x6E20D400 | (2<<16)|(2<<5)|2 = 0x6E22D442
	WORD  $0x6E22D442
	// FADDP S2, V2.2S = 0x7E30D800 | (2<<5)|2 = 0x7E30D842
	WORD  $0x7E30D842
	FMOVS F2, 8(R0)

	// FADDP V3.4S, V3.4S, V3.4S = 0x6E20D400 | (3<<16)|(3<<5)|3 = 0x6E23D463
	WORD  $0x6E23D463
	// FADDP S3, V3.2S = 0x7E30D800 | (3<<5)|3 = 0x7E30D863
	WORD  $0x7E30D863
	FMOVS F3, 12(R0)

	// Scalar tail K%4 — pointers already advanced past main loop
	MOVD  K+24(FP), R3
	AND   $3, R3, R9
	CBZ   R9, md4_done

md4_tail:
	FMOVS (R2), F4            // bk
	FMOVS (R5), F5
	FMULS F4, F5, F5
	FMOVS (R0), F6
	FADDS F5, F6, F6
	FMOVS F6, (R0)

	FMOVS (R6), F5
	FMULS F4, F5, F5
	FMOVS 4(R0), F6
	FADDS F5, F6, F6
	FMOVS F6, 4(R0)

	FMOVS (R7), F5
	FMULS F4, F5, F5
	FMOVS 8(R0), F6
	FADDS F5, F6, F6
	FMOVS F6, 8(R0)

	FMOVS (R8), F5
	FMULS F4, F5, F5
	FMOVS 12(R0), F6
	FADDS F5, F6, F6
	FMOVS F6, 12(R0)

	ADD   $4, R2, R2
	ADD   $4, R5, R5
	ADD   $4, R6, R6
	ADD   $4, R7, R7
	ADD   $4, R8, R8
	SUB   $1, R9, R9
	CBNZ  R9, md4_tail

md4_done:
	RET

// func multiDot8NEON(out *[8]float32, a, b *float32, K int)
// Same as multiDot4 but 8 A rows — B loaded once per chunk into 8 FMA chains.
// Frame: out+0, a+8, b+16, K+24
TEXT ·multiDot8NEON(SB), NOSPLIT, $0-32
	MOVD  out+0(FP), R0
	MOVD  a+8(FP), R1
	MOVD  b+16(FP), R2
	MOVD  K+24(FP), R3

	LSL   $2, R3, R4          // row stride bytes

	// Row bases R5..R12
	MOVD  R1, R5
	ADD   R4, R5, R6
	ADD   R4, R6, R7
	ADD   R4, R7, R8
	ADD   R4, R8, R9
	ADD   R4, R9, R10
	ADD   R4, R10, R11
	ADD   R4, R11, R12

	// Zero V0..V7
	VEOR  V0.B16, V0.B16, V0.B16
	VEOR  V1.B16, V1.B16, V1.B16
	VEOR  V2.B16, V2.B16, V2.B16
	VEOR  V3.B16, V3.B16, V3.B16
	VEOR  V4.B16, V4.B16, V4.B16
	VEOR  V5.B16, V5.B16, V5.B16
	VEOR  V6.B16, V6.B16, V6.B16
	VEOR  V7.B16, V7.B16, V7.B16

	LSR   $2, R3, R13
	CBZ   R13, md8_hsum

md8_loop:
	// V16 = B chunk (keep V0-V7 as accums)
	VLD1.P 16(R2), [V16.S4]

	VLD1.P 16(R5), [V17.S4]
	// FMLA V0.4S, V17.4S, V16.4S: Rd=0 Rn=17 Rm=16
	// = 0x4E20CC00 | (16<<16) | (17<<5) | 0 = 0x4E30CC00 | 0x220 = 0x4E30CE20
	WORD  $0x4E30CE20

	VLD1.P 16(R6), [V17.S4]
	// FMLA V1.4S, V17.4S, V16.4S: Rd=1 = 0x4E30CE21
	WORD  $0x4E30CE21

	VLD1.P 16(R7), [V17.S4]
	WORD  $0x4E30CE22         // FMLA V2

	VLD1.P 16(R8), [V17.S4]
	WORD  $0x4E30CE23         // FMLA V3

	VLD1.P 16(R9), [V17.S4]
	WORD  $0x4E30CE24         // FMLA V4

	VLD1.P 16(R10), [V17.S4]
	WORD  $0x4E30CE25         // FMLA V5

	VLD1.P 16(R11), [V17.S4]
	WORD  $0x4E30CE26         // FMLA V6

	VLD1.P 16(R12), [V17.S4]
	WORD  $0x4E30CE27         // FMLA V7

	SUB   $1, R13, R13
	CBNZ  R13, md8_loop

md8_hsum:
	// hsum V0..V7 → out[0..7]
	WORD  $0x6E20D400         // FADDP V0.4S, V0.4S, V0.4S
	WORD  $0x7E30D800         // FADDP S0, V0.2S
	FMOVS F0, (R0)

	WORD  $0x6E21D421         // FADDP V1
	WORD  $0x7E30D821
	FMOVS F1, 4(R0)

	WORD  $0x6E22D442
	WORD  $0x7E30D842
	FMOVS F2, 8(R0)

	WORD  $0x6E23D463
	WORD  $0x7E30D863
	FMOVS F3, 12(R0)

	// FADDP V4.4S, V4.4S, V4.4S = 0x6E20D400 | (4<<16)|(4<<5)|4 = 0x6E24D484
	WORD  $0x6E24D484
	// FADDP S4, V4.2S = 0x7E30D800 | (4<<5)|4 = 0x7E30D884
	WORD  $0x7E30D884
	FMOVS F4, 16(R0)

	// FADDP V5 = 0x6E20D400 | (5<<16)|(5<<5)|5 = 0x6E25D4A5
	WORD  $0x6E25D4A5
	WORD  $0x7E30D8A5         // FADDP S5, V5.2S
	FMOVS F5, 20(R0)

	// FADDP V6 = 0x6E20D400 | (6<<16)|(6<<5)|6 = 0x6E26D4C6
	WORD  $0x6E26D4C6
	WORD  $0x7E30D8C6
	FMOVS F6, 24(R0)

	// FADDP V7 = 0x6E20D400 | (7<<16)|(7<<5)|7 = 0x6E27D4E7
	WORD  $0x6E27D4E7
	WORD  $0x7E30D8E7
	FMOVS F7, 28(R0)

	// Scalar tail K%4
	MOVD  K+24(FP), R3
	AND   $3, R3, R13
	CBZ   R13, md8_done

md8_tail:
	FMOVS (R2), F16           // bk
	// row0
	FMOVS (R5), F17
	FMULS F16, F17, F17
	FMOVS (R0), F18
	FADDS F17, F18, F18
	FMOVS F18, (R0)
	// row1
	FMOVS (R6), F17
	FMULS F16, F17, F17
	FMOVS 4(R0), F18
	FADDS F17, F18, F18
	FMOVS F18, 4(R0)
	// row2
	FMOVS (R7), F17
	FMULS F16, F17, F17
	FMOVS 8(R0), F18
	FADDS F17, F18, F18
	FMOVS F18, 8(R0)
	// row3
	FMOVS (R8), F17
	FMULS F16, F17, F17
	FMOVS 12(R0), F18
	FADDS F17, F18, F18
	FMOVS F18, 12(R0)
	// row4
	FMOVS (R9), F17
	FMULS F16, F17, F17
	FMOVS 16(R0), F18
	FADDS F17, F18, F18
	FMOVS F18, 16(R0)
	// row5
	FMOVS (R10), F17
	FMULS F16, F17, F17
	FMOVS 20(R0), F18
	FADDS F17, F18, F18
	FMOVS F18, 20(R0)
	// row6
	FMOVS (R11), F17
	FMULS F16, F17, F17
	FMOVS 24(R0), F18
	FADDS F17, F18, F18
	FMOVS F18, 24(R0)
	// row7
	FMOVS (R12), F17
	FMULS F16, F17, F17
	FMOVS 28(R0), F18
	FADDS F17, F18, F18
	FMOVS F18, 28(R0)

	ADD   $4, R2, R2
	ADD   $4, R5, R5
	ADD   $4, R6, R6
	ADD   $4, R7, R7
	ADD   $4, R8, R8
	ADD   $4, R9, R9
	ADD   $4, R10, R10
	ADD   $4, R11, R11
	ADD   $4, R12, R12
	SUB   $1, R13, R13
	CBNZ  R13, md8_tail

md8_done:
	RET

// func multiDot4DualBNEON(out *[8]float32, a, b0, b1 *float32, K int)
// out[0:4]=dots(a,b0), out[4:8]=dots(a,b1); A loaded once per 4-float chunk.
// Frame: out+0, a+8, b0+16, b1+24, K+32
TEXT ·multiDot4DualBNEON(SB), NOSPLIT, $0-40
	MOVD  out+0(FP), R0
	MOVD  a+8(FP), R1
	MOVD  b0+16(FP), R2
	MOVD  b1+24(FP), R3
	MOVD  K+32(FP), R4

	LSL   $2, R4, R5          // stride
	MOVD  R1, R6
	ADD   R5, R6, R7
	ADD   R5, R7, R8
	ADD   R5, R8, R9

	// V0-V3 = b0 acc, V4-V7 = b1 acc
	VEOR  V0.B16, V0.B16, V0.B16
	VEOR  V1.B16, V1.B16, V1.B16
	VEOR  V2.B16, V2.B16, V2.B16
	VEOR  V3.B16, V3.B16, V3.B16
	VEOR  V4.B16, V4.B16, V4.B16
	VEOR  V5.B16, V5.B16, V5.B16
	VEOR  V6.B16, V6.B16, V6.B16
	VEOR  V7.B16, V7.B16, V7.B16

	LSR   $2, R4, R10
	CBZ   R10, dual_neon_hsum

dual_neon_loop:
	VLD1.P 16(R2), [V16.S4]   // b0
	VLD1.P 16(R3), [V17.S4]   // b1

	VLD1.P 16(R6), [V18.S4]
	// FMLA V0.4S, V18.4S, V16.4S: Rd=0 Rn=18 Rm=16
	// = 0x4E20CC00 | (16<<16) | (18<<5) | 0 = 0x4E30CC00 | 0x240 = 0x4E30CE40
	WORD  $0x4E30CE40
	// FMLA V4.4S, V18.4S, V17.4S: Rd=4 Rn=18 Rm=17
	// = 0x4E20CC00 | (17<<16) | (18<<5) | 4 = 0x4E31CC00 | 0x244 = 0x4E31CE44
	WORD  $0x4E31CE44

	VLD1.P 16(R7), [V18.S4]
	WORD  $0x4E30CE41         // FMLA V1, V18, V16
	WORD  $0x4E31CE45         // FMLA V5, V18, V17

	VLD1.P 16(R8), [V18.S4]
	WORD  $0x4E30CE42         // FMLA V2
	WORD  $0x4E31CE46         // FMLA V6

	VLD1.P 16(R9), [V18.S4]
	WORD  $0x4E30CE43         // FMLA V3
	WORD  $0x4E31CE47         // FMLA V7

	SUB   $1, R10, R10
	CBNZ  R10, dual_neon_loop

dual_neon_hsum:
	WORD  $0x6E20D400
	WORD  $0x7E30D800
	FMOVS F0, (R0)
	WORD  $0x6E21D421
	WORD  $0x7E30D821
	FMOVS F1, 4(R0)
	WORD  $0x6E22D442
	WORD  $0x7E30D842
	FMOVS F2, 8(R0)
	WORD  $0x6E23D463
	WORD  $0x7E30D863
	FMOVS F3, 12(R0)
	WORD  $0x6E24D484
	WORD  $0x7E30D884
	FMOVS F4, 16(R0)
	WORD  $0x6E25D4A5
	WORD  $0x7E30D8A5
	FMOVS F5, 20(R0)
	WORD  $0x6E26D4C6
	WORD  $0x7E30D8C6
	FMOVS F6, 24(R0)
	WORD  $0x6E27D4E7
	WORD  $0x7E30D8E7
	FMOVS F7, 28(R0)

	// Scalar tail K%4
	MOVD  K+32(FP), R4
	AND   $3, R4, R10
	CBZ   R10, dual_neon_done

dual_neon_tail:
	FMOVS (R2), F16
	FMOVS (R3), F17
	// row0
	FMOVS (R6), F18
	FMULS F16, F18, F19
	FMOVS (R0), F20
	FADDS F19, F20, F20
	FMOVS F20, (R0)
	FMULS F17, F18, F19
	FMOVS 16(R0), F20
	FADDS F19, F20, F20
	FMOVS F20, 16(R0)
	// row1
	FMOVS (R7), F18
	FMULS F16, F18, F19
	FMOVS 4(R0), F20
	FADDS F19, F20, F20
	FMOVS F20, 4(R0)
	FMULS F17, F18, F19
	FMOVS 20(R0), F20
	FADDS F19, F20, F20
	FMOVS F20, 20(R0)
	// row2
	FMOVS (R8), F18
	FMULS F16, F18, F19
	FMOVS 8(R0), F20
	FADDS F19, F20, F20
	FMOVS F20, 8(R0)
	FMULS F17, F18, F19
	FMOVS 24(R0), F20
	FADDS F19, F20, F20
	FMOVS F20, 24(R0)
	// row3
	FMOVS (R9), F18
	FMULS F16, F18, F19
	FMOVS 12(R0), F20
	FADDS F19, F20, F20
	FMOVS F20, 12(R0)
	FMULS F17, F18, F19
	FMOVS 28(R0), F20
	FADDS F19, F20, F20
	FMOVS F20, 28(R0)

	ADD   $4, R2, R2
	ADD   $4, R3, R3
	ADD   $4, R6, R6
	ADD   $4, R7, R7
	ADD   $4, R8, R8
	ADD   $4, R9, R9
	SUB   $1, R10, R10
	CBNZ  R10, dual_neon_tail

dual_neon_done:
	RET
