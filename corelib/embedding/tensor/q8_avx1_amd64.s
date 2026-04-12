//go:build amd64

#include "textflag.h"

// func dotQ8RowAVX1(a []float32, data []byte, rowOff, nBlocks int) float32
//
// AVX1-only version of dotQ8Row for Sandy/Ivy Bridge CPUs.
// Differences from AVX2 version:
//   - Uses PMOVSXBD (SSE4.1, 128-bit) instead of VPMOVSXBD (AVX2)
//   - Uses VINSERTF128 instead of VINSERTI128
//   - Uses VMULPS+VADDPS instead of VFMADD231PS (no FMA3)
//   - Uses PSHUFD+VINSERTF128 instead of VBROADCASTSS xmm,ymm (AVX2)
//
TEXT ·dotQ8RowAVX1(SB), NOSPLIT, $0-68
	MOVQ a_base+0(FP), SI
	MOVQ data_base+24(FP), DI
	MOVQ rowOff+48(FP), R8
	MOVQ nBlocks+56(FP), CX
	ADDQ R8, DI

	VXORPS Y0, Y0, Y0

	TESTQ CX, CX
	JZ    avx1_dot_done

avx1_dot_loop:
	// --- f16 scale → f32, broadcast ---
	MOVWLZX (DI), AX
	MOVL  AX, DX
	SHRL  $15, DX
	SHLL  $31, DX
	MOVL  AX, R9
	SHRL  $10, R9
	ANDL  $0x1f, R9
	MOVL  AX, R10
	ANDL  $0x3ff, R10
	TESTL R9, R9
	JZ    avx1_dot_sz
	CMPL  R9, $31
	JE    avx1_dot_si
	ADDL  $112, R9
	SHLL  $23, R9
	SHLL  $13, R10
	ORL   R9, DX
	ORL   R10, DX
	JMP   avx1_dot_sr

avx1_dot_sz:
	JMP   avx1_dot_sr

avx1_dot_si:
	ORL   $0x7f800000, DX
	SHLL  $13, R10
	ORL   R10, DX

avx1_dot_sr:
	// Broadcast scale to YMM using AVX1-safe sequence:
	// MOVD → PSHUFD (128-bit broadcast) → VINSERTF128 (copy to high half)
	MOVL  DX, X1
	PSHUFD $0, X1, X1          // X1 = [scale, scale, scale, scale]
	VINSERTF128 $1, X1, Y1, Y1 // Y1 = [scale x8]

	// Group 0: bytes [2..9] → 8 int8 → 8 float32
	PMOVSXBD 2(DI), X2
	PMOVSXBD 6(DI), X3
	VINSERTF128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   (SI), Y3
	VMULPS    Y2, Y3, Y4
	VADDPS    Y4, Y0, Y0

	// Group 1: bytes [10..17]
	PMOVSXBD 10(DI), X2
	PMOVSXBD 14(DI), X3
	VINSERTF128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   32(SI), Y3
	VMULPS    Y2, Y3, Y4
	VADDPS    Y4, Y0, Y0

	// Group 2: bytes [18..25]
	PMOVSXBD 18(DI), X2
	PMOVSXBD 22(DI), X3
	VINSERTF128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   64(SI), Y3
	VMULPS    Y2, Y3, Y4
	VADDPS    Y4, Y0, Y0

	// Group 3: bytes [26..33]
	PMOVSXBD 26(DI), X2
	PMOVSXBD 30(DI), X3
	VINSERTF128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   96(SI), Y3
	VMULPS    Y2, Y3, Y4
	VADDPS    Y4, Y0, Y0

	ADDQ  $34, DI
	ADDQ  $128, SI
	DECQ  CX
	JNZ   avx1_dot_loop

avx1_dot_done:
	VEXTRACTF128 $1, Y0, X1
	VADDPS X1, X0, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0
	MOVSS X0, ret+64(FP)
	VZEROUPPER
	RET

// func dequantRowIntoAVX1(dst []float32, data []byte, rowOff, nBlocks int)
//
// AVX1-only dequantization of Q8_0 row.
//
TEXT ·dequantRowIntoAVX1(SB), NOSPLIT, $0-64
	MOVQ dst_base+0(FP), SI
	MOVQ data_base+24(FP), DI
	MOVQ rowOff+48(FP), R8
	MOVQ nBlocks+56(FP), CX
	ADDQ R8, DI

	TESTQ CX, CX
	JZ    avx1_dq_done

avx1_dq_loop:
	MOVWLZX (DI), AX
	MOVL  AX, DX
	SHRL  $15, DX
	SHLL  $31, DX
	MOVL  AX, R9
	SHRL  $10, R9
	ANDL  $0x1f, R9
	MOVL  AX, R10
	ANDL  $0x3ff, R10
	TESTL R9, R9
	JZ    avx1_dq_sz
	CMPL  R9, $31
	JE    avx1_dq_si
	ADDL  $112, R9
	SHLL  $23, R9
	SHLL  $13, R10
	ORL   R9, DX
	ORL   R10, DX
	JMP   avx1_dq_sr

avx1_dq_sz:
	JMP   avx1_dq_sr

avx1_dq_si:
	ORL   $0x7f800000, DX
	SHLL  $13, R10
	ORL   R10, DX

avx1_dq_sr:
	MOVL  DX, X1
	PSHUFD $0, X1, X1
	VINSERTF128 $1, X1, Y1, Y1

	// Group 0
	PMOVSXBD 2(DI), X2
	PMOVSXBD 6(DI), X3
	VINSERTF128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, (SI)

	// Group 1
	PMOVSXBD 10(DI), X2
	PMOVSXBD 14(DI), X3
	VINSERTF128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 32(SI)

	// Group 2
	PMOVSXBD 18(DI), X2
	PMOVSXBD 22(DI), X3
	VINSERTF128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 64(SI)

	// Group 3
	PMOVSXBD 26(DI), X2
	PMOVSXBD 30(DI), X3
	VINSERTF128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 96(SI)

	ADDQ  $34, DI
	ADDQ  $128, SI
	DECQ  CX
	JNZ   avx1_dq_loop

avx1_dq_done:
	VZEROUPPER
	RET
