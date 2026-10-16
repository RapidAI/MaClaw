//go:build amd64

#include "textflag.h"

// func gemmaQuantizeQ8UAVX512(q *byte, s *float32, a *float32, rows, nBlocks int)
// Signed f32 activations → per-32-block u8(+128) quantization, row-major:
//   q[r*K + b*32 + i] = clamp(round(a[r][b*32+i]*inv), -127, 127) + 128
//   s[r*nBlocks + b]  = amax(block) / 127
// K = nBlocks*32. Branchless near-zero handling via eps clamp (q stays 128,
// s≈0, both multiply out to 0 in the kernel).
TEXT ·gemmaQuantizeQ8UAVX512(SB), NOSPLIT, $16-40
	MOVQ q+0(FP), DI
	MOVQ s+8(FP), R8
	MOVQ a+16(FP), SI
	MOVQ rows+24(FP), R9
	MOVQ nBlocks+32(FP), R12

	MOVL $0x42fe0000, AX // 127.0f
	MOVL AX, 0(SP)
	MOVL $0x34000000, AX // ~1.19e-7f eps
	MOVL AX, 4(SP)
	MOVL $0x3c010204, AX // 1/127.0f
	MOVL AX, 8(SP)
	MOVL $0x7fffffff, AX // abs mask
	MOVL AX, 12(SP)
	VBROADCASTSS 0(SP), Z14
	VMOVSS 4(SP), X15
	VMOVSS 8(SP), X13
	VPBROADCASTD 12(SP), Z12
	MOVL $0x80, AX
	MOVL AX, 12(SP)
	VPBROADCASTD 12(SP), Z11 // +128 per dword (low byte survives VPMOVDB)

gq_row:
	MOVQ R12, CX
	MOVQ SI, R10
	MOVQ DI, R11
	MOVQ R8, R13

gq_blk:
	VMOVDQU32 (R10), Z0
	VMOVDQU32 64(R10), Z1
	VPANDD Z12, Z0, Z2
	VPANDD Z12, Z1, Z3
	VMAXPS Z3, Z2, Z2
	VEXTRACTF32X8 $1, Z2, Y3
	VMAXPS Y2, Y3, Y2
	VEXTRACTF32X4 $1, Y2, X3
	VMAXPS X2, X3, X2
	VSHUFPD $1, X2, X2, X3
	VMAXPS X2, X3, X2
	VMOVSHDUP X2, X3
	VMAXSS X2, X3, X2    // X2 = amax
	VMULSS X13, X2, X4   // scale = amax/127
	VMAXSS X15, X2, X3   // amax' = max(amax, eps)
	VRCP14SS X3, X3, X3
	VMULSS X14, X3, X3   // inv = 127/amax'
	VBROADCASTSS X3, Z5
	VMOVSS X4, (R13)
	VMULPS Z5, Z0, Z0
	VMULPS Z5, Z1, Z1
	VCVTPS2DQ Z0, Z0
	VCVTPS2DQ Z1, Z1
	VPADDD Z11, Z0, Z0
	VPADDD Z11, Z1, Z1
	VPMOVDB Z0, X6
	VPMOVDB Z1, X7
	VMOVUPS X6, (R11)
	VMOVUPS X7, 16(R11)
	ADDQ $128, R10
	ADDQ $32, R11
	ADDQ $4, R13
	DECQ CX
	JNZ gq_blk

	// advance one row: a += nBlocks*128, q += nBlocks*32, s += nBlocks*4
	MOVQ R12, AX
	SHLQ $7, AX
	ADDQ AX, SI
	MOVQ R12, AX
	SHLQ $5, AX
	ADDQ AX, DI
	LEAQ (R8)(R12*4), R8
	DECQ R9
	JNZ gq_row
	RET

// func gemmaQuantizeQ8URowAVX512(q *byte, s *float32, a *float32, rows, nBlocks int)
// Per-ROW scale variant for the row-scale VNNI M8 kernels:
//   s[r] = rowAmax/127 (single f32 per row)
//   q[r*K + i] = round(a[r][i]*127/amax) + 128
// Pass 1 finds the row absmax, pass 2 quantizes (row stays L1-hot).
TEXT ·gemmaQuantizeQ8URowAVX512(SB), NOSPLIT, $16-40
	MOVQ q+0(FP), DI
	MOVQ s+8(FP), R8
	MOVQ a+16(FP), SI
	MOVQ rows+24(FP), R9
	MOVQ nBlocks+32(FP), R12

	MOVL $0x42fe0000, AX // 127.0f
	MOVL AX, 0(SP)
	MOVL $0x34000000, AX // ~1.19e-7f eps
	MOVL AX, 4(SP)
	MOVL $0x3c010204, AX // 1/127.0f
	MOVL AX, 8(SP)
	MOVL $0x7fffffff, AX
	MOVL AX, 12(SP)
	VBROADCASTSS 0(SP), Z14
	VMOVSS 4(SP), X15
	VMOVSS 8(SP), X13
	VPBROADCASTD 12(SP), Z12
	MOVL $0x80, AX
	MOVL AX, 12(SP)
	VPBROADCASTD 12(SP), Z11

gqr_row:
	// pass 1: absmax over nBlocks*2 zmm
	MOVQ R12, CX
	ADDQ CX, CX
	MOVQ SI, R10
	VXORPS Z2, Z2, Z2
gqr_max:
	VMOVDQU32 (R10), Z0
	VPANDD Z12, Z0, Z0
	VMAXPS Z0, Z2, Z2
	ADDQ $64, R10
	DECQ CX
	JNZ gqr_max
	VEXTRACTF32X8 $1, Z2, Y3
	VMAXPS Y2, Y3, Y2
	VEXTRACTF32X4 $1, Y2, X3
	VMAXPS X2, X3, X2
	VSHUFPD $1, X2, X2, X3
	VMAXPS X2, X3, X2
	VMOVSHDUP X2, X3
	VMAXSS X2, X3, X2  // amax
	VMULSS X13, X2, X4 // scale = amax/127
	VMAXSS X15, X2, X3
	VRCP14SS X3, X3, X3
	VMULSS X14, X3, X3 // inv = 127/amax'
	VMOVSS X4, (R8)
	VBROADCASTSS X3, Z5

	// pass 2: quantize nBlocks blocks
	MOVQ R12, CX
	MOVQ SI, R10
	MOVQ DI, R11
gqr_blk:
	VMOVDQU32 (R10), Z0
	VMOVDQU32 64(R10), Z1
	VMULPS Z5, Z0, Z0
	VMULPS Z5, Z1, Z1
	VCVTPS2DQ Z0, Z0
	VCVTPS2DQ Z1, Z1
	VPADDD Z11, Z0, Z0
	VPADDD Z11, Z1, Z1
	VPMOVDB Z0, X6
	VPMOVDB Z1, X7
	VMOVUPS X6, (R11)
	VMOVUPS X7, 16(R11)
	ADDQ $128, R10
	ADDQ $32, R11
	DECQ CX
	JNZ gqr_blk

	// advance one row: a += nBlocks*128, q += nBlocks*32, s += 4
	MOVQ R12, AX
	SHLQ $7, AX
	ADDQ AX, SI
	MOVQ R12, AX
	SHLQ $5, AX
	ADDQ AX, DI
	ADDQ $4, R8
	DECQ R9
	JNZ gqr_row
	RET
