//go:build arm64

#include "textflag.h"

// SDOT Vd.4S, Vn.16B, Vm.16B = 0x4E809400 | Rm<<16 | Rn<<5 | Rd
// DUP  Vd.4S, Vn.S[0]       = 0x4E040400 | Rn<<5 | Rd
// SCVTF Vd.4S, Vn.4S        = 0x4E21D800 | Rn<<5 | Rd
// FMLA  Vd.4S, Vn.4S, Vm.4S = 0x4E20CC00 | Rm<<16 | Rn<<5 | Rd
// FMUL  Vd.4S, Vn.4S, Vm.4S = 0x6E20DC00 | Rm<<16 | Rn<<5 | Rd

// func q8pTile4x32SDOT(out *float32, aq *int8, kStride int, as *float32, nbStride int, qb *byte, sb *float32, nb int)
// 4×32 tile, s8×s8 via SDOT. Processes eight groups of 4 columns.
TEXT ·q8pTile4x32SDOT(SB), NOSPLIT, $0-64
	MOVD out+0(FP), R0
	MOVD aq+8(FP), R1
	MOVD kStride+16(FP), R2
	MOVD as+24(FP), R3
	MOVD nbStride+32(FP), R4
	MOVD qb+40(FP), R5
	MOVD sb+48(FP), R6
	MOVD nb+56(FP), R7

	// row act pointers: R1, R8, R9, R10
	ADD R2, R1, R8
	ADD R2, R8, R9
	ADD R2, R9, R10
	// 3*nbStride in R11 for row3 scales
	ADD R4, R4, R11
	ADD R4, R11, R11

	MOVD ZR, R12 // col group 0..7
grp:
	// reset act ptrs
	MOVD aq+8(FP), R1
	MOVD kStride+16(FP), R2
	ADD R2, R1, R8
	ADD R2, R8, R9
	ADD R2, R9, R10
	MOVD as+24(FP), R3
	// qb/sb + group*16 bytes (4 cols × 4B)
	MOVD R12, R13
	LSL $4, R13, R13
	MOVD qb+40(FP), R5
	ADD R13, R5, R5
	MOVD sb+48(FP), R6
	ADD R13, R6, R6
	MOVD nb+56(FP), R7

	// float acc V16..V19 = 0 (4 rows × 4 cols)
	VEOR V16.B16, V16.B16, V16.B16
	VEOR V17.B16, V17.B16, V17.B16
	VEOR V18.B16, V18.B16, V18.B16
	VEOR V19.B16, V19.B16, V19.B16

blk:
	// int acc V0..V3 = 0
	VEOR V0.B16, V0.B16, V0.B16
	VEOR V1.B16, V1.B16, V1.B16
	VEOR V2.B16, V2.B16, V2.B16
	VEOR V3.B16, V3.B16, V3.B16
	MOVD $8, R14 // K steps
kloop:
	VLD1 (R5), [V4.B16] // 4 cols × 4 s8 weights
	// row 0
	MOVW (R1), R15
	FMOVS R15, F5
	WORD $0x4E0404A5 // DUP V5.4S, V5.S[0]
	WORD $0x4E8494A0 // SDOT V0.4S, V5.16B, V4.16B
	// row 1
	MOVW (R8), R15
	FMOVS R15, F5
	WORD $0x4E0404A5
	WORD $0x4E8494A1 // SDOT V1.4S, V5.16B, V4.16B
	// row 2
	MOVW (R9), R15
	FMOVS R15, F5
	WORD $0x4E0404A5
	WORD $0x4E8494A2
	// row 3
	MOVW (R10), R15
	FMOVS R15, F5
	WORD $0x4E0404A5
	WORD $0x4E8494A3
	ADD $4, R1, R1
	ADD $4, R8, R8
	ADD $4, R9, R9
	ADD $4, R10, R10
	ADD $128, R5, R5 // next K-step, same col group
	SUB $1, R14, R14
	CBNZ R14, kloop

	// scales: load 4 weight scales, broadcast act scale, FMA
	VLD1 (R6), [V6.S4] // 4 weight scales
	FMOVS (R3), F7
	WORD $0x4E0404E7 // DUP V7.4S, V7.S[0]
	WORD $0x6E26DCE7 // FMUL V7.4S, V7.4S, V6.4S  (sa*sw)
	WORD $0x4E21D800 // SCVTF V0.4S, V0.4S
	WORD $0x4E27CC10 // FMLA V16.4S, V0.4S, V7.4S

	ADD R4, R3, R15
	FMOVS (R15), F7
	WORD $0x4E0404E7
	WORD $0x6E26DCE7
	WORD $0x4E21D821
	WORD $0x4E27CC31

	ADD R4, R15, R15
	FMOVS (R15), F7
	WORD $0x4E0404E7
	WORD $0x6E26DCE7
	WORD $0x4E21D842
	WORD $0x4E27CC52

	ADD R4, R15, R15
	FMOVS (R15), F7
	WORD $0x4E0404E7
	WORD $0x6E26DCE7
	WORD $0x4E21D863
	WORD $0x4E27CC73

	ADD $4, R3, R3
	ADD $128, R6, R6
	SUB $1, R7, R7
	CBNZ R7, blk

	// store 4×4 at out[m*32 + group*4]
	MOVD R12, R13
	LSL $4, R13, R13 // group*4 floats * 4 bytes = group*16
	MOVD out+0(FP), R0
	ADD R13, R0, R0
	VST1 [V16.S4], (R0)
	ADD $128, R0, R0 // +32 floats
	VST1 [V17.S4], (R0)
	ADD $128, R0, R0
	VST1 [V18.S4], (R0)
	ADD $128, R0, R0
	VST1 [V19.S4], (R0)

	ADD $1, R12, R12
	CMP $8, R12
	BLT grp
	RET
