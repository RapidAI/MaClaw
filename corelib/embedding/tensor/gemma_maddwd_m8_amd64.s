//go:build amd64

#include "textflag.h"

// Row-scale AVX2 VPMADDWD M8 kernels for EmbeddingGemma medium/large sequences
// on AVX2-only machines (no AVX-512 VNNI). Activations are prequantized to
// s16 with one f32 scale per row:
//   out[r][n] = aS[r] · Σ_blk bS[n][blk]·Σ_i a16[r][i]·int8(w8[n][i])
// Weights are read straight from the Q8_0 layout: each 32-block is 34 bytes
// (2-byte f16 scale skipped + 32-byte s8 payload), block scale from bS.
//
// Why s16 activations + VPMADDWD (not u8 + VPMADDUBSW): maddubs needs one
// side unsigned ≤127 and saturates, which signed activations cannot use
// without a correction term; VPMADDWD is exact (no saturation).
// Overflow analysis: |a16| ≤ 16383, |w8| ≤ 127, so each maddwd lane output
// (sum of 2 products) is ≤ 2·16383·127 ≈ 4.16M; accumulating K/32 ≤ 36 blocks
// in s32 gives ≤ 36·4.16M ≈ 150M ≪ 2^31. Safe.
//
// Inner loop: the k-loop is unrolled ×2 (24/36 blocks are even), which cuts
// loop-control overhead ~2× and lets VPMOVSXBW use its memory operand form
// (no separate VMOVDQU). A row's a16 halves are consumed exactly once per
// block, so the two VPMADDWD read them straight from memory as well (no
// VMOVDQU): per 32-weight block per column that is 2 VPMOVSXBW (mem) + 1
// VBROADCASTSS (block scale) shared across the 8 rows, then per row
// 2 VPMADDWD (mem) + 1 VPADDD + 1 VCVTDQ2PS + 1 VFMADD231PS — 48 instructions
// per block total (~21% fewer than the register-operand form), leaving the
// Zen4 FP0/FP1 multiply pipes (16 VPMADDWD + 8 VFMADD231PS = 12 cycles per
// block) as the sole bottleneck.
//
// Epilogue: the 8 row accumulators are reduced with a shuffle tree
// (fold 256→128, then two VSHUFPS+VADDPS levels) instead of 8 independent
// horizontal-sum chains, and the row scales aS[0..7] are applied as two
// vector multiplies, so one column retires in ~55 instructions instead of
// ~95; the strided row stores use VEXTRACTPS to memory.
//
// aQ: 8 rows × K int16, row stride K*2 bytes. aS: 8 f32, stride 4.
// bData: Q8_0 block rows (nBlocks*34 bytes per B row). bS: nBlocks f32 per row.
// The column loop covers single columns n in [ns, ne).

// func gemmaMaddwdRowM8N24AVX2(out *float32, aQ *int16, aS *float32, bData *byte, bS *float32, N, ns, ne int)
// K=768 (24 blocks). B row stride 816 bytes; bS row stride 96 bytes.
TEXT ·gemmaMaddwdRowM8N24AVX2(SB), NOSPLIT, $64-64
	MOVQ out+0(FP), AX
	MOVQ AX, 0(SP)
	MOVQ aQ+8(FP), AX
	MOVQ AX, 8(SP)
	MOVQ aS+16(FP), AX
	MOVQ AX, 16(SP)
	MOVQ bData+24(FP), AX
	MOVQ AX, 24(SP)
	MOVQ bS+32(FP), AX
	MOVQ AX, 32(SP)
	MOVQ N+40(FP), AX
	MOVQ AX, 40(SP)
	MOVQ ne+56(FP), AX
	MOVQ AX, 48(SP)
	MOVQ ns+48(FP), AX
	MOVQ AX, 56(SP)

	MOVQ 24(SP), DI
	MOVQ AX, R12
	IMULQ $816, R12
	ADDQ R12, DI
	MOVQ 32(SP), R13
	MOVQ AX, R12
	IMULQ $96, R12
	ADDQ R12, R13
	MOVQ 8(SP), SI
	MOVQ 16(SP), R8

r8m24n:
	MOVQ 56(SP), R12
	MOVQ 48(SP), CX
	CMPQ R12, CX
	JGE  r8m24done

	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3
	VXORPS Y4, Y4, Y4
	VXORPS Y5, Y5, Y5
	VXORPS Y6, Y6, Y6
	VXORPS Y7, Y7, Y7
	MOVQ   $12, CX

r8m24k:
	PREFETCHT0    256(DI)
	PREFETCHT0    324(DI)
	VPMOVSXBW     2(DI), Y8  // w16 block 2i, elements 0-15
	VPMOVSXBW     18(DI), Y9 // w16 block 2i, elements 16-31
	VBROADCASTSS  (R13), Y10
	// row 0
	VPMADDWD      (SI), Y8, Y13
	VPMADDWD      32(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y0
	// row 1
	VPMADDWD      1536(SI), Y8, Y13
	VPMADDWD      1568(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y1
	// row 2
	VPMADDWD      3072(SI), Y8, Y13
	VPMADDWD      3104(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y2
	// row 3
	VPMADDWD      4608(SI), Y8, Y13
	VPMADDWD      4640(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y3
	// row 4
	VPMADDWD      6144(SI), Y8, Y13
	VPMADDWD      6176(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y4
	// row 5
	VPMADDWD      7680(SI), Y8, Y13
	VPMADDWD      7712(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y5
	// row 6
	VPMADDWD      9216(SI), Y8, Y13
	VPMADDWD      9248(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y6
	// row 7
	VPMADDWD      10752(SI), Y8, Y13
	VPMADDWD      10784(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y7
	// block 2i+1: weights at DI+36/+52, scale 4(R13), A halves at +64
	VPMOVSXBW     36(DI), Y8
	VPMOVSXBW     52(DI), Y9
	VBROADCASTSS  4(R13), Y10
	// row 0
	VPMADDWD      64(SI), Y8, Y13
	VPMADDWD      96(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y0
	// row 1
	VPMADDWD      1600(SI), Y8, Y13
	VPMADDWD      1632(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y1
	// row 2
	VPMADDWD      3136(SI), Y8, Y13
	VPMADDWD      3168(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y2
	// row 3
	VPMADDWD      4672(SI), Y8, Y13
	VPMADDWD      4704(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y3
	// row 4
	VPMADDWD      6208(SI), Y8, Y13
	VPMADDWD      6240(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y4
	// row 5
	VPMADDWD      7744(SI), Y8, Y13
	VPMADDWD      7776(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y5
	// row 6
	VPMADDWD      9280(SI), Y8, Y13
	VPMADDWD      9312(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y6
	// row 7
	VPMADDWD      10816(SI), Y8, Y13
	VPMADDWD      10848(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y7

	ADDQ $128, SI
	ADDQ $68, DI
	ADDQ $8, R13
	DECQ CX
	JNZ  r8m24k

	// finish 8 rows: shuffle-tree hsum → ×aS[r] → store out[r*N+n]
	MOVQ 0(SP), BX
	MOVQ 56(SP), AX
	MOVQ 40(SP), DX
	MOVQ AX, R12
	SHLQ $2, R12
	ADDQ BX, R12

	// fold 256→128: Xr = row r partial sums (4 lanes each)
	VEXTRACTF128 $1, Y0, X8
	VADDPS       X8, X0, X0
	VEXTRACTF128 $1, Y1, X8
	VADDPS       X8, X1, X1
	VEXTRACTF128 $1, Y2, X8
	VADDPS       X8, X2, X2
	VEXTRACTF128 $1, Y3, X8
	VADDPS       X8, X3, X3
	VEXTRACTF128 $1, Y4, X8
	VADDPS       X8, X4, X4
	VEXTRACTF128 $1, Y5, X8
	VADDPS       X8, X5, X5
	VEXTRACTF128 $1, Y6, X8
	VADDPS       X8, X6, X6
	VEXTRACTF128 $1, Y7, X8
	VADDPS       X8, X7, X7

	// level 1: pair rows; X0=[r0a r0b r1a r1b] etc.
	VSHUFPS $0x44, X1, X0, X8
	VSHUFPS $0xEE, X1, X0, X9
	VADDPS  X9, X8, X0
	VSHUFPS $0x44, X3, X2, X8
	VSHUFPS $0xEE, X3, X2, X9
	VADDPS  X9, X8, X2
	VSHUFPS $0x44, X5, X4, X8
	VSHUFPS $0xEE, X5, X4, X9
	VADDPS  X9, X8, X4
	VSHUFPS $0x44, X7, X6, X8
	VSHUFPS $0xEE, X7, X6, X9
	VADDPS  X9, X8, X6

	// level 2: X0 = [s0 s1 s2 s3], X4 = [s4 s5 s6 s7]
	VSHUFPS $0x88, X2, X0, X8
	VSHUFPS $0xDD, X2, X0, X9
	VADDPS  X9, X8, X0
	VSHUFPS $0x88, X6, X4, X8
	VSHUFPS $0xDD, X6, X4, X9
	VADDPS  X9, X8, X4

	// × row scales (aS[0..7] contiguous)
	VMOVUPS (R8), X8
	VMOVUPS 16(R8), X9
	VMULPS  X8, X0, X0
	VMULPS  X9, X4, X4

	// strided row stores
	VEXTRACTPS $0, X0, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $1, X0, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $2, X0, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $3, X0, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $0, X4, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $1, X4, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $2, X4, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $3, X4, (R12)

	ADDQ $1, 56(SP)
	SUBQ $1536, SI
	JMP  r8m24n

r8m24done:
	VZEROUPPER
	RET

// func gemmaMaddwdRowM8N36AVX2(out *float32, aQ *int16, aS *float32, bData *byte, bS *float32, N, ns, ne int)
// K=1152 (36 blocks). B row stride 1224 bytes; bS row stride 144 bytes.
TEXT ·gemmaMaddwdRowM8N36AVX2(SB), NOSPLIT, $64-64
	MOVQ out+0(FP), AX
	MOVQ AX, 0(SP)
	MOVQ aQ+8(FP), AX
	MOVQ AX, 8(SP)
	MOVQ aS+16(FP), AX
	MOVQ AX, 16(SP)
	MOVQ bData+24(FP), AX
	MOVQ AX, 24(SP)
	MOVQ bS+32(FP), AX
	MOVQ AX, 32(SP)
	MOVQ N+40(FP), AX
	MOVQ AX, 40(SP)
	MOVQ ne+56(FP), AX
	MOVQ AX, 48(SP)
	MOVQ ns+48(FP), AX
	MOVQ AX, 56(SP)

	MOVQ 24(SP), DI
	MOVQ AX, R12
	IMULQ $1224, R12
	ADDQ R12, DI
	MOVQ 32(SP), R13
	MOVQ AX, R12
	IMULQ $144, R12
	ADDQ R12, R13
	MOVQ 8(SP), SI
	MOVQ 16(SP), R8

r8m36n:
	MOVQ 56(SP), R12
	MOVQ 48(SP), CX
	CMPQ R12, CX
	JGE  r8m36done

	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3
	VXORPS Y4, Y4, Y4
	VXORPS Y5, Y5, Y5
	VXORPS Y6, Y6, Y6
	VXORPS Y7, Y7, Y7
	MOVQ   $18, CX

r8m36k:
	PREFETCHT0    256(DI)
	PREFETCHT0    324(DI)
	VPMOVSXBW     2(DI), Y8
	VPMOVSXBW     18(DI), Y9
	VBROADCASTSS  (R13), Y10
	// row 0
	VPMADDWD      (SI), Y8, Y13
	VPMADDWD      32(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y0
	// row 1
	VPMADDWD      2304(SI), Y8, Y13
	VPMADDWD      2336(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y1
	// row 2
	VPMADDWD      4608(SI), Y8, Y13
	VPMADDWD      4640(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y2
	// row 3
	VPMADDWD      6912(SI), Y8, Y13
	VPMADDWD      6944(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y3
	// row 4
	VPMADDWD      9216(SI), Y8, Y13
	VPMADDWD      9248(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y4
	// row 5
	VPMADDWD      11520(SI), Y8, Y13
	VPMADDWD      11552(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y5
	// row 6
	VPMADDWD      13824(SI), Y8, Y13
	VPMADDWD      13856(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y6
	// row 7
	VPMADDWD      16128(SI), Y8, Y13
	VPMADDWD      16160(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y7
	// block 2i+1: weights at DI+36/+52, scale 4(R13), A halves at +64
	VPMOVSXBW     36(DI), Y8
	VPMOVSXBW     52(DI), Y9
	VBROADCASTSS  4(R13), Y10
	// row 0
	VPMADDWD      64(SI), Y8, Y13
	VPMADDWD      96(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y0
	// row 1
	VPMADDWD      2368(SI), Y8, Y13
	VPMADDWD      2400(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y1
	// row 2
	VPMADDWD      4672(SI), Y8, Y13
	VPMADDWD      4704(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y2
	// row 3
	VPMADDWD      6976(SI), Y8, Y13
	VPMADDWD      7008(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y3
	// row 4
	VPMADDWD      9280(SI), Y8, Y13
	VPMADDWD      9312(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y4
	// row 5
	VPMADDWD      11584(SI), Y8, Y13
	VPMADDWD      11616(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y5
	// row 6
	VPMADDWD      13888(SI), Y8, Y13
	VPMADDWD      13920(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y6
	// row 7
	VPMADDWD      16192(SI), Y8, Y13
	VPMADDWD      16224(SI), Y9, Y14
	VPADDD        Y14, Y13, Y15
	VCVTDQ2PS     Y15, Y15
	VFMADD231PS   Y15, Y10, Y7

	ADDQ $128, SI
	ADDQ $68, DI
	ADDQ $8, R13
	DECQ CX
	JNZ  r8m36k

	// finish 8 rows: shuffle-tree hsum → ×aS[r] → store out[r*N+n]
	MOVQ 0(SP), BX
	MOVQ 56(SP), AX
	MOVQ 40(SP), DX
	MOVQ AX, R12
	SHLQ $2, R12
	ADDQ BX, R12

	VEXTRACTF128 $1, Y0, X8
	VADDPS       X8, X0, X0
	VEXTRACTF128 $1, Y1, X8
	VADDPS       X8, X1, X1
	VEXTRACTF128 $1, Y2, X8
	VADDPS       X8, X2, X2
	VEXTRACTF128 $1, Y3, X8
	VADDPS       X8, X3, X3
	VEXTRACTF128 $1, Y4, X8
	VADDPS       X8, X4, X4
	VEXTRACTF128 $1, Y5, X8
	VADDPS       X8, X5, X5
	VEXTRACTF128 $1, Y6, X8
	VADDPS       X8, X6, X6
	VEXTRACTF128 $1, Y7, X8
	VADDPS       X8, X7, X7

	VSHUFPS $0x44, X1, X0, X8
	VSHUFPS $0xEE, X1, X0, X9
	VADDPS  X9, X8, X0
	VSHUFPS $0x44, X3, X2, X8
	VSHUFPS $0xEE, X3, X2, X9
	VADDPS  X9, X8, X2
	VSHUFPS $0x44, X5, X4, X8
	VSHUFPS $0xEE, X5, X4, X9
	VADDPS  X9, X8, X4
	VSHUFPS $0x44, X7, X6, X8
	VSHUFPS $0xEE, X7, X6, X9
	VADDPS  X9, X8, X6

	VSHUFPS $0x88, X2, X0, X8
	VSHUFPS $0xDD, X2, X0, X9
	VADDPS  X9, X8, X0
	VSHUFPS $0x88, X6, X4, X8
	VSHUFPS $0xDD, X6, X4, X9
	VADDPS  X9, X8, X4

	VMOVUPS (R8), X8
	VMOVUPS 16(R8), X9
	VMULPS  X8, X0, X0
	VMULPS  X9, X4, X4

	VEXTRACTPS $0, X0, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $1, X0, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $2, X0, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $3, X0, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $0, X4, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $1, X4, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $2, X4, (R12)
	LEAQ       (R12)(DX*4), R12
	VEXTRACTPS $3, X4, (R12)

	ADDQ $1, 56(SP)
	SUBQ $2304, SI
	JMP  r8m36n

r8m36done:
	VZEROUPPER
	RET

// func gemmaQuantizeS16AVX2(q *int16, s *float32, a *float32, rows, nBlocks int)
// Signed f32 activations → per-ROW s16 quantization, row-major:
//   s[r]    = rowAmax/16383
//   q[r][i] = round(a[r][i]*16383/amax')   (nearest-even; |q| ≤ 16383)
// K = nBlocks*32. Pass 1 finds the row absmax (8-lane VANDPS abs + hmax),
// pass 2 scales, converts to s32 (VCVTPS2DQ) and packs pairs of 16 dwords to
// s16 with VPACKSSDW + VPERMQ $0xD8 lane fixup. Near-zero rows are handled
// branchlessly: amax' = max(amax, eps) keeps inv finite, q = round(0) = 0,
// s = amax/16383 ≈ 0, both multiply out to ~0 in the kernel.
TEXT ·gemmaQuantizeS16AVX2(SB), NOSPLIT, $16-40
	MOVQ q+0(FP), DI
	MOVQ s+8(FP), R8
	MOVQ a+16(FP), SI
	MOVQ rows+24(FP), R9
	MOVQ nBlocks+32(FP), R12

	MOVL $0x467FFC00, AX // 16383.0f
	MOVL AX, 0(SP)
	VBROADCASTSS 0(SP), Y13
	MOVL $0x34000000, AX // ~1.19e-7f eps
	MOVL AX, 4(SP)
	VMOVSS 4(SP), X15
	MOVL $0x7fffffff, AX // abs mask
	MOVL AX, 8(SP)
	VBROADCASTSS 8(SP), Y12

gqs_row:
	// pass 1: absmax over nBlocks*4 ymm (8 floats per ymm)
	MOVQ  R12, CX
	ADDQ  CX, CX
	ADDQ  CX, CX
	MOVQ  SI, R10
	VXORPS Y2, Y2, Y2
gqs_max:
	VMOVDQU (R10), Y0
	VANDPS  Y12, Y0, Y0
	VMAXPS  Y0, Y2, Y2
	ADDQ    $32, R10
	DECQ    CX
	JNZ     gqs_max
	VEXTRACTF128 $1, Y2, X3
	VMAXPS       X3, X2, X2
	VSHUFPD      $1, X2, X2, X3
	VMAXPS       X3, X2, X2
	VMOVSHDUP    X2, X3
	VMAXSS       X3, X2, X2  // amax
	VDIVSS       X13, X2, X4 // s = amax/16383
	VMAXSS       X15, X2, X3 // amax' = max(amax, eps)
	VDIVSS       X3, X13, X3 // inv = 16383/amax'
	VMOVSS       X4, (R8)
	VBROADCASTSS X3, Y5

	// pass 2: quantize 2*nBlocks halves (16 elements = 2 ymm per iteration)
	MOVQ R12, CX
	ADDQ CX, CX
	MOVQ SI, R10
	MOVQ DI, R11
gqs_blk:
	VMOVDQU   (R10), Y0
	VMOVDQU   32(R10), Y1
	VMULPS    Y5, Y0, Y0
	VMULPS    Y5, Y1, Y1
	VCVTPS2DQ Y0, Y0
	VCVTPS2DQ Y1, Y1
	// VPACKSSDW lane-interleaves: [e0-3,e8-11 | e4-7,e12-15];
	// VPERMQ 0xD8 restores linear [e0-3,e4-7,e8-11,e12-15]
	VPACKSSDW Y1, Y0, Y2
	VPERMQ    $0xD8, Y2, Y2
	VMOVDQU   Y2, (R11)
	ADDQ      $64, R10
	ADDQ      $32, R11
	DECQ      CX
	JNZ       gqs_blk

	// advance one row: a += nBlocks*128, q += nBlocks*64, s += 4
	MOVQ R12, AX
	SHLQ $7, AX
	ADDQ AX, SI
	MOVQ R12, AX
	SHLQ $6, AX
	ADDQ AX, DI
	ADDQ $4, R8
	DECQ R9
	JNZ  gqs_row
	VZEROUPPER
	RET
