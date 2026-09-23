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
// Per 32-weight block per column: 1 load w8 (32B) + 2 VPMOVSXBW (w8→w16)
// + 1 VBROADCASTSS (block scale) shared across the 8 rows, then per row
// 2 VMOVDQU (a16) + 2 VPMADDWD + 1 VPADDD + 1 VCVTDQ2PS + 1 VFMADD231PS —
// ~7 instructions per row per 32 MAC vs ~28 for the f32 (convert+FMA) route.
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
	MOVQ   $24, CX

r8m24k:
	PREFETCHT0 256(DI)
	VMOVDQU     2(DI), X8  // w8 block payload, low 16 bytes
	VPMOVSXBW   X8, Y8     // w16 elements 0-15
	VMOVDQU     18(DI), X9 // w8 block payload, high 16 bytes
	VPMOVSXBW   X9, Y9     // w16 elements 16-31
	VBROADCASTSS (R13), Y10 // block scale bS[n][blk]
	// row 0
	VMOVDQU     (SI), Y11
	VMOVDQU     32(SI), Y12
	VPMADDWD    Y11, Y8, Y13
	VPMADDWD    Y12, Y9, Y14
	VPADDD      Y14, Y13, Y15
	VCVTDQ2PS   Y15, Y15
	VFMADD231PS Y15, Y10, Y0
	// row 1
	VMOVDQU     1536(SI), Y11
	VMOVDQU     1568(SI), Y12
	VPMADDWD    Y11, Y8, Y13
	VPMADDWD    Y12, Y9, Y14
	VPADDD      Y14, Y13, Y15
	VCVTDQ2PS   Y15, Y15
	VFMADD231PS Y15, Y10, Y1
	// row 2
	VMOVDQU     3072(SI), Y11
	VMOVDQU     3104(SI), Y12
	VPMADDWD    Y11, Y8, Y13
	VPMADDWD    Y12, Y9, Y14
	VPADDD      Y14, Y13, Y15
	VCVTDQ2PS   Y15, Y15
	VFMADD231PS Y15, Y10, Y2
	// row 3
	VMOVDQU     4608(SI), Y11
	VMOVDQU     4640(SI), Y12
	VPMADDWD    Y11, Y8, Y13
	VPMADDWD    Y12, Y9, Y14
	VPADDD      Y14, Y13, Y15
	VCVTDQ2PS   Y15, Y15
	VFMADD231PS Y15, Y10, Y3
	// row 4
	VMOVDQU     6144(SI), Y11
	VMOVDQU     6176(SI), Y12
	VPMADDWD    Y11, Y8, Y13
	VPMADDWD    Y12, Y9, Y14
	VPADDD      Y14, Y13, Y15
	VCVTDQ2PS   Y15, Y15
	VFMADD231PS Y15, Y10, Y4
	// row 5
	VMOVDQU     7680(SI), Y11
	VMOVDQU     7712(SI), Y12
	VPMADDWD    Y11, Y8, Y13
	VPMADDWD    Y12, Y9, Y14
	VPADDD      Y14, Y13, Y15
	VCVTDQ2PS   Y15, Y15
	VFMADD231PS Y15, Y10, Y5
	// row 6
	VMOVDQU     9216(SI), Y11
	VMOVDQU     9248(SI), Y12
	VPMADDWD    Y11, Y8, Y13
	VPMADDWD    Y12, Y9, Y14
	VPADDD      Y14, Y13, Y15
	VCVTDQ2PS   Y15, Y15
	VFMADD231PS Y15, Y10, Y6
	// row 7
	VMOVDQU     10752(SI), Y11
	VMOVDQU     10784(SI), Y12
	VPMADDWD    Y11, Y8, Y13
	VPMADDWD    Y12, Y9, Y14
	VPADDD      Y14, Y13, Y15
	VCVTDQ2PS   Y15, Y15
	VFMADD231PS Y15, Y10, Y7

	ADDQ $64, SI
	ADDQ $34, DI
	ADDQ $4, R13
	DECQ CX
	JNZ  r8m24k

	// finish 8 rows: hsum(acc) → ×aS[r] → store out[r*N+n]
	MOVQ 0(SP), BX
	MOVQ 56(SP), AX
	MOVQ 40(SP), DX
	MOVQ AX, R12
	SHLQ $2, R12
	ADDQ BX, R12

	VEXTRACTF128 $1, Y0, X8
	VADDPS       X8, X0, X0
	VSHUFPD      $1, X0, X0, X8
	VADDPS       X8, X0, X0
	VMOVSHDUP    X0, X8
	VADDSS       X8, X0, X0
	VMULSS       (R8), X0, X0
	VMOVSS       X0, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y1, X8
	VADDPS       X8, X1, X1
	VSHUFPD      $1, X1, X1, X8
	VADDPS       X8, X1, X1
	VMOVSHDUP    X1, X8
	VADDSS       X8, X1, X1
	VMULSS       4(R8), X1, X1
	VMOVSS       X1, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y2, X8
	VADDPS       X8, X2, X2
	VSHUFPD      $1, X2, X2, X8
	VADDPS       X8, X2, X2
	VMOVSHDUP    X2, X8
	VADDSS       X8, X2, X2
	VMULSS       8(R8), X2, X2
	VMOVSS       X2, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y3, X8
	VADDPS       X8, X3, X3
	VSHUFPD      $1, X3, X3, X8
	VADDPS       X8, X3, X3
	VMOVSHDUP    X3, X8
	VADDSS       X8, X3, X3
	VMULSS       12(R8), X3, X3
	VMOVSS       X3, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y4, X8
	VADDPS       X8, X4, X4
	VSHUFPD      $1, X4, X4, X8
	VADDPS       X8, X4, X4
	VMOVSHDUP    X4, X8
	VADDSS       X8, X4, X4
	VMULSS       16(R8), X4, X4
	VMOVSS       X4, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y5, X8
	VADDPS       X8, X5, X5
	VSHUFPD      $1, X5, X5, X8
	VADDPS       X8, X5, X5
	VMOVSHDUP    X5, X8
	VADDSS       X8, X5, X5
	VMULSS       20(R8), X5, X5
	VMOVSS       X5, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y6, X8
	VADDPS       X8, X6, X6
	VSHUFPD      $1, X6, X6, X8
	VADDPS       X8, X6, X6
	VMOVSHDUP    X6, X8
	VADDSS       X8, X6, X6
	VMULSS       24(R8), X6, X6
	VMOVSS       X6, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y7, X8
	VADDPS       X8, X7, X7
	VSHUFPD      $1, X7, X7, X8
	VADDPS       X8, X7, X7
	VMOVSHDUP    X7, X8
	VADDSS       X8, X7, X7
	VMULSS       28(R8), X7, X7
	VMOVSS       X7, (R12)

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
	MOVQ   $36, CX

r8m36k:
	PREFETCHT0 256(DI)
	VMOVDQU      2(DI), X8
	VPMOVSXBW    X8, Y8
	VMOVDQU      18(DI), X9
	VPMOVSXBW    X9, Y9
	VBROADCASTSS (R13), Y10
	// row 0
	VMOVDQU      (SI), Y11
	VMOVDQU      32(SI), Y12
	VPMADDWD     Y11, Y8, Y13
	VPMADDWD     Y12, Y9, Y14
	VPADDD       Y14, Y13, Y15
	VCVTDQ2PS    Y15, Y15
	VFMADD231PS  Y15, Y10, Y0
	// row 1
	VMOVDQU      2304(SI), Y11
	VMOVDQU      2336(SI), Y12
	VPMADDWD     Y11, Y8, Y13
	VPMADDWD     Y12, Y9, Y14
	VPADDD       Y14, Y13, Y15
	VCVTDQ2PS    Y15, Y15
	VFMADD231PS  Y15, Y10, Y1
	// row 2
	VMOVDQU      4608(SI), Y11
	VMOVDQU      4640(SI), Y12
	VPMADDWD     Y11, Y8, Y13
	VPMADDWD     Y12, Y9, Y14
	VPADDD       Y14, Y13, Y15
	VCVTDQ2PS    Y15, Y15
	VFMADD231PS  Y15, Y10, Y2
	// row 3
	VMOVDQU      6912(SI), Y11
	VMOVDQU      6944(SI), Y12
	VPMADDWD     Y11, Y8, Y13
	VPMADDWD     Y12, Y9, Y14
	VPADDD       Y14, Y13, Y15
	VCVTDQ2PS    Y15, Y15
	VFMADD231PS  Y15, Y10, Y3
	// row 4
	VMOVDQU      9216(SI), Y11
	VMOVDQU      9248(SI), Y12
	VPMADDWD     Y11, Y8, Y13
	VPMADDWD     Y12, Y9, Y14
	VPADDD       Y14, Y13, Y15
	VCVTDQ2PS    Y15, Y15
	VFMADD231PS  Y15, Y10, Y4
	// row 5
	VMOVDQU      11520(SI), Y11
	VMOVDQU      11552(SI), Y12
	VPMADDWD     Y11, Y8, Y13
	VPMADDWD     Y12, Y9, Y14
	VPADDD       Y14, Y13, Y15
	VCVTDQ2PS    Y15, Y15
	VFMADD231PS  Y15, Y10, Y5
	// row 6
	VMOVDQU      13824(SI), Y11
	VMOVDQU      13856(SI), Y12
	VPMADDWD     Y11, Y8, Y13
	VPMADDWD     Y12, Y9, Y14
	VPADDD       Y14, Y13, Y15
	VCVTDQ2PS    Y15, Y15
	VFMADD231PS  Y15, Y10, Y6
	// row 7
	VMOVDQU      16128(SI), Y11
	VMOVDQU      16160(SI), Y12
	VPMADDWD     Y11, Y8, Y13
	VPMADDWD     Y12, Y9, Y14
	VPADDD       Y14, Y13, Y15
	VCVTDQ2PS    Y15, Y15
	VFMADD231PS  Y15, Y10, Y7

	ADDQ $64, SI
	ADDQ $34, DI
	ADDQ $4, R13
	DECQ CX
	JNZ  r8m36k

	MOVQ 0(SP), BX
	MOVQ 56(SP), AX
	MOVQ 40(SP), DX
	MOVQ AX, R12
	SHLQ $2, R12
	ADDQ BX, R12

	VEXTRACTF128 $1, Y0, X8
	VADDPS       X8, X0, X0
	VSHUFPD      $1, X0, X0, X8
	VADDPS       X8, X0, X0
	VMOVSHDUP    X0, X8
	VADDSS       X8, X0, X0
	VMULSS       (R8), X0, X0
	VMOVSS       X0, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y1, X8
	VADDPS       X8, X1, X1
	VSHUFPD      $1, X1, X1, X8
	VADDPS       X8, X1, X1
	VMOVSHDUP    X1, X8
	VADDSS       X8, X1, X1
	VMULSS       4(R8), X1, X1
	VMOVSS       X1, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y2, X8
	VADDPS       X8, X2, X2
	VSHUFPD      $1, X2, X2, X8
	VADDPS       X8, X2, X2
	VMOVSHDUP    X2, X8
	VADDSS       X8, X2, X2
	VMULSS       8(R8), X2, X2
	VMOVSS       X2, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y3, X8
	VADDPS       X8, X3, X3
	VSHUFPD      $1, X3, X3, X8
	VADDPS       X8, X3, X3
	VMOVSHDUP    X3, X8
	VADDSS       X8, X3, X3
	VMULSS       12(R8), X3, X3
	VMOVSS       X3, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y4, X8
	VADDPS       X8, X4, X4
	VSHUFPD      $1, X4, X4, X8
	VADDPS       X8, X4, X4
	VMOVSHDUP    X4, X8
	VADDSS       X8, X4, X4
	VMULSS       16(R8), X4, X4
	VMOVSS       X4, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y5, X8
	VADDPS       X8, X5, X5
	VSHUFPD      $1, X5, X5, X8
	VADDPS       X8, X5, X5
	VMOVSHDUP    X5, X8
	VADDSS       X8, X5, X5
	VMULSS       20(R8), X5, X5
	VMOVSS       X5, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y6, X8
	VADDPS       X8, X6, X6
	VSHUFPD      $1, X6, X6, X8
	VADDPS       X8, X6, X6
	VMOVSHDUP    X6, X8
	VADDSS       X8, X6, X6
	VMULSS       24(R8), X6, X6
	VMOVSS       X6, (R12)
	LEAQ         (R12)(DX*4), R12

	VEXTRACTF128 $1, Y7, X8
	VADDPS       X8, X7, X7
	VSHUFPD      $1, X7, X7, X8
	VADDPS       X8, X7, X7
	VMOVSHDUP    X7, X8
	VADDSS       X8, X7, X7
	VMULSS       28(R8), X7, X7
	VMOVSS       X7, (R12)

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
