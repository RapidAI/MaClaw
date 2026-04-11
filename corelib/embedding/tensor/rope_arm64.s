//go:build arm64

#include "textflag.h"

// ARM64 NEON WORD encodings:
// FMUL Vd.4S, Vn.4S, Vm.4S = 0x6E20DC00 | Rm<<16 | Rn<<5 | Rd
// FSUB Vd.4S, Vn.4S, Vm.4S = 0x4EA0D400 | Rm<<16 | Rn<<5 | Rd
// FADD Vd.4S, Vn.4S, Vm.4S = 0x4E20D400 | Rm<<16 | Rn<<5 | Rd
// FMLA Vd.4S, Vn.4S, Vm.4S = 0x4E20CC00 | Rm<<16 | Rn<<5 | Rd

// func ropePrecomputedASM(x []float32, nHeads, headDim int, cosTable, sinTable []float32)
TEXT ·ropePrecomputedASM(SB), NOSPLIT, $0-88
	MOVD  x+0(FP), R0
	MOVD  nHeads+24(FP), R1
	MOVD  headDim+32(FP), R2
	MOVD  cosTable+40(FP), R3
	MOVD  sinTable+64(FP), R4

	LSR   $1, R2, R5          // R5 = halfDim
	LSL   $2, R2, R6          // R6 = headDim * 4
	LSL   $2, R5, R7          // R7 = halfDim * 4

	CBZ   R1, rope_done

	MOVD  R0, R8              // R8 = head pointer

rope_head:
	ADD   R7, R8, R9          // R9 = x1 region
	MOVD  R5, R10             // R10 = halfDim remaining
	MOVD  R3, R11             // cos ptr
	MOVD  R4, R12             // sin ptr
	MOVD  R8, R13             // x0 ptr
	MOVD  R9, R14             // x1 ptr

	LSR   $2, R10, R15        // R15 = halfDim / 4
	CBZ   R15, rope_tail

rope_inner4:
	VLD1.P 16(R11), [V0.S4]  // cos
	VLD1.P 16(R12), [V1.S4]  // sin
	VLD1  (R13), [V2.S4]     // x0
	VLD1  (R14), [V3.S4]     // x1

	// V4 = x0*cos: FMUL V4.4S, V2.4S, V0.4S
	// = 0x6E20DC00 | (0<<16) | (2<<5) | 4 = 0x6E20DC44
	WORD  $0x6E20DC44
	// V5 = x1*sin: FMUL V5.4S, V3.4S, V1.4S
	// = 0x6E20DC00 | (1<<16) | (3<<5) | 5 = 0x6E21DC65
	WORD  $0x6E21DC65
	// V4 = V4 - V5: FSUB V4.4S, V4.4S, V5.4S
	// = 0x4EA0D400 | (5<<16) | (4<<5) | 4 = 0x4EA5D484
	WORD  $0x4EA5D484

	// V5 = x0*sin: FMUL V5.4S, V2.4S, V1.4S
	// = 0x6E20DC00 | (1<<16) | (2<<5) | 5 = 0x6E21DC45
	WORD  $0x6E21DC45
	// V5 += x1*cos: FMLA V5.4S, V3.4S, V0.4S
	// = 0x4E20CC00 | (0<<16) | (3<<5) | 5 = 0x4E20CC65
	WORD  $0x4E20CC65

	VST1.P [V4.S4], 16(R13)
	VST1.P [V5.S4], 16(R14)

	SUB   $1, R15, R15
	CBNZ  R15, rope_inner4

rope_tail:
	AND   $3, R10, R10
	CBZ   R10, rope_next

rope_scalar:
	FMOVS (R11), F0           // cos
	FMOVS (R12), F1           // sin
	FMOVS (R13), F2           // x0
	FMOVS (R14), F3           // x1

	FMULS F0, F2, F4          // x0*cos
	FMULS F1, F3, F5          // x1*sin
	FSUBS F5, F4, F4          // x0*cos - x1*sin
	FMOVS F4, (R13)

	FMULS F1, F2, F5          // x0*sin
	FMULS F0, F3, F6          // x1*cos
	FADDS F6, F5, F5          // x0*sin + x1*cos
	FMOVS F5, (R14)

	ADD   $4, R11, R11
	ADD   $4, R12, R12
	ADD   $4, R13, R13
	ADD   $4, R14, R14
	SUB   $1, R10, R10
	CBNZ  R10, rope_scalar

rope_next:
	ADD   R6, R8, R8
	SUB   $1, R1, R1
	CBNZ  R1, rope_head

rope_done:
	RET
