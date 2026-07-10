//go:build amd64

#include "textflag.h"

// func dotQ8RowScaledAVX2(a *float32, data *byte, scales *float32, rowOff, nBlocks int) float32
// Frame: a+0, data+8, scales+16, rowOff+24, nBlocks+32, ret+40 = 44 → pad 48
TEXT ·dotQ8RowScaledAVX2(SB), NOSPLIT, $0-48
	MOVQ a+0(FP), SI
	MOVQ data+8(FP), DI
	MOVQ scales+16(FP), R8
	MOVQ rowOff+24(FP), R9
	MOVQ nBlocks+32(FP), CX
	ADDQ R9, DI

	VXORPS Y0, Y0, Y0
	TESTQ CX, CX
	JZ    dqs_dot_done

dqs_dot_loop:
	PREFETCHT0 34(DI)
	PREFETCHT0 4(R8)
	PREFETCHT0 128(SI)

	MOVSS (R8), X1
	VBROADCASTSS X1, Y1
	ADDQ  $4, R8

	VPMOVSXBD 2(DI), X2
	VPMOVSXBD 6(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   (SI), Y3
	VFMADD231PS Y2, Y3, Y0

	VPMOVSXBD 10(DI), X2
	VPMOVSXBD 14(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   32(SI), Y3
	VFMADD231PS Y2, Y3, Y0

	VPMOVSXBD 18(DI), X2
	VPMOVSXBD 22(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   64(SI), Y3
	VFMADD231PS Y2, Y3, Y0

	VPMOVSXBD 26(DI), X2
	VPMOVSXBD 30(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   96(SI), Y3
	VFMADD231PS Y2, Y3, Y0

	ADDQ  $34, DI
	ADDQ  $128, SI
	DECQ  CX
	JNZ   dqs_dot_loop

dqs_dot_done:
	VEXTRACTF128 $1, Y0, X1
	VADDPS X1, X0, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0
	MOVSS X0, ret+40(FP)
	VZEROUPPER
	RET

// func q8DualDot2ScaledAVX2(out *[2]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)
// Frame: out+0, a+8, data+16, scales0+24, scales1+32, rowOff0+40, rowOff1+48, nBlocks+56 = 64
TEXT ·q8DualDot2ScaledAVX2(SB), NOSPLIT, $0-64
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ data+16(FP), DI
	MOVQ scales0+24(FP), R8
	MOVQ scales1+32(FP), R9
	MOVQ nBlocks+56(FP), CX

	ADDQ rowOff0+40(FP), DI
	MOVQ data+16(FP), R15
	ADDQ rowOff1+48(FP), R15

	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	TESTQ CX, CX
	JZ    dd_sc_hsum

dd_sc_loop:
	PREFETCHT0 34(DI)
	PREFETCHT0 34(R15)

	MOVSS (R8), X14
	VBROADCASTSS X14, Y14
	ADDQ  $4, R8
	MOVSS (R9), X15
	VBROADCASTSS X15, Y15
	ADDQ  $4, R9

	// g0
	VPMOVSXBD 2(DI), X2
	VPMOVSXBD 6(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y14, Y2, Y2
	VPMOVSXBD 2(R15), X3
	VPMOVSXBD 6(R15), X4
	VINSERTI128 $1, X4, Y3, Y3
	VCVTDQ2PS Y3, Y3
	VMULPS Y15, Y3, Y3
	VMOVUPS (SI), Y4
	VFMADD231PS Y4, Y2, Y0
	VFMADD231PS Y4, Y3, Y1

	// g1
	VPMOVSXBD 10(DI), X2
	VPMOVSXBD 14(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y14, Y2, Y2
	VPMOVSXBD 10(R15), X3
	VPMOVSXBD 14(R15), X4
	VINSERTI128 $1, X4, Y3, Y3
	VCVTDQ2PS Y3, Y3
	VMULPS Y15, Y3, Y3
	VMOVUPS 32(SI), Y4
	VFMADD231PS Y4, Y2, Y0
	VFMADD231PS Y4, Y3, Y1

	// g2
	VPMOVSXBD 18(DI), X2
	VPMOVSXBD 22(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y14, Y2, Y2
	VPMOVSXBD 18(R15), X3
	VPMOVSXBD 22(R15), X4
	VINSERTI128 $1, X4, Y3, Y3
	VCVTDQ2PS Y3, Y3
	VMULPS Y15, Y3, Y3
	VMOVUPS 64(SI), Y4
	VFMADD231PS Y4, Y2, Y0
	VFMADD231PS Y4, Y3, Y1

	// g3
	VPMOVSXBD 26(DI), X2
	VPMOVSXBD 30(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y14, Y2, Y2
	VPMOVSXBD 26(R15), X3
	VPMOVSXBD 30(R15), X4
	VINSERTI128 $1, X4, Y3, Y3
	VCVTDQ2PS Y3, Y3
	VMULPS Y15, Y3, Y3
	VMOVUPS 96(SI), Y4
	VFMADD231PS Y4, Y2, Y0
	VFMADD231PS Y4, Y3, Y1

	ADDQ $34, DI
	ADDQ $34, R15
	ADDQ $128, SI
	DECQ CX
	JNZ  dd_sc_loop

dd_sc_hsum:
	VEXTRACTF128 $1, Y0, X2
	VADDPS X0, X2, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0
	MOVSS X0, (R11)

	VEXTRACTF128 $1, Y1, X2
	VADDPS X1, X2, X1
	VHADDPS X1, X1, X1
	VHADDPS X1, X1, X1
	MOVSS X1, 4(R11)

	VZEROUPPER
	RET

// func dotQ8RowAVX2(a []float32, data []byte, rowOff, nBlocks int) float32
//
// Computes dot product of float32 vector a[] with a Q8_0 row.
// Q8_0 block layout: [scale:f16(2 bytes)][d0..d31:int8(32 bytes)] = 34 bytes.
// Uses AVX2+FMA: vpmovsxbd (int8→int32), vcvtdq2ps (int32→f32), vfmadd231ps.
//
// Arguments (Go ABI internal):
//   a.ptr      = +0(FP)   (pointer to float32 data)
//   a.len      = +8(FP)
//   a.cap      = +16(FP)
//   data.ptr   = +24(FP)  (pointer to byte data)
//   data.len   = +32(FP)
//   data.cap   = +40(FP)
//   rowOff     = +48(FP)
//   nBlocks    = +56(FP)
//   ret        = +64(FP)
TEXT ·dotQ8RowAVX2(SB), NOSPLIT, $0-68
	MOVQ a+0(FP), SI          // SI = &a[0]
	MOVQ data+24(FP), DI      // DI = &data[0]
	MOVQ rowOff+48(FP), R8    // R8 = rowOff (byte offset)
	MOVQ nBlocks+56(FP), CX   // CX = nBlocks (loop counter)
	ADDQ R8, DI               // DI = &data[rowOff] (start of row)

	// Zero the accumulator: Y0 = sum[0..7]
	VXORPS Y0, Y0, Y0

	TESTQ CX, CX
	JZ    done

loop:
	// Load scale (float16) from first 2 bytes of block
	MOVWLZX (DI), AX          // AX = f16 bits (zero-extended to 32-bit)
	// Convert f16 to f32 inline:
	// sign = (h >> 15) << 31
	// exp  = (h >> 10) & 0x1f
	// mant = h & 0x3ff
	// f32  = sign | (exp+112)<<23 | mant<<13  (for normal values)
	MOVL  AX, DX
	SHRL  $15, DX
	SHLL  $31, DX              // DX = sign bit in f32 position
	MOVL  AX, R9
	SHRL  $10, R9
	ANDL  $0x1f, R9            // R9 = exponent (5 bits)
	MOVL  AX, R10
	ANDL  $0x3ff, R10          // R10 = mantissa (10 bits)
	// Handle zero exponent (zero or subnormal) — for Q8 scales, subnormals are rare
	TESTL R9, R9
	JZ    scale_zero
	CMPL  R9, $31
	JE    scale_inf
	ADDL  $112, R9             // bias adjust: f16 bias(15) → f32 bias(127)
	SHLL  $23, R9
	SHLL  $13, R10
	ORL   R9, DX
	ORL   R10, DX              // DX = f32 bits
	JMP   scale_ready

scale_zero:
	// exp=0: if mant=0 → ±0, else subnormal (treat as 0 for Q8 scales)
	// DX already has sign|0 which is ±0
	JMP   scale_ready

scale_inf:
	// exp=31: infinity/NaN — shouldn't happen for valid Q8 scales
	ORL   $0x7f800000, DX
	SHLL  $13, R10
	ORL   R10, DX

scale_ready:
	// Broadcast scale to all 8 lanes of Y1
	MOVL  DX, X1
	VBROADCASTSS X1, Y1       // Y1 = [scale, scale, ..., scale]

	// Process 32 int8 values in 4 groups of 8
	// Group 0: bytes [2..9]
	VPMOVSXBD 2(DI), X2       // X2 = sign-extend 4 bytes → 4 int32
	VPMOVSXBD 6(DI), X3       // X3 = next 4 bytes
	VINSERTI128 $1, X3, Y2, Y2 // Y2 = 8 int32 values
	VCVTDQ2PS Y2, Y2          // Y2 = 8 float32 values
	VMULPS    Y1, Y2, Y2      // Y2 = scale * int8_values
	// a[0..7] * dequantized[0..7]
	VMOVUPS   (SI), Y3
	VFMADD231PS Y2, Y3, Y0    // Y0 += Y3 * Y2

	// Group 1: bytes [10..17]
	VPMOVSXBD 10(DI), X2
	VPMOVSXBD 14(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   32(SI), Y3
	VFMADD231PS Y2, Y3, Y0

	// Group 2: bytes [18..25]
	VPMOVSXBD 18(DI), X2
	VPMOVSXBD 22(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   64(SI), Y3
	VFMADD231PS Y2, Y3, Y0

	// Group 3: bytes [26..33]
	VPMOVSXBD 26(DI), X2
	VPMOVSXBD 30(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   96(SI), Y3
	VFMADD231PS Y2, Y3, Y0

	// Advance pointers
	ADDQ  $34, DI              // next Q8 block (34 bytes)
	ADDQ  $128, SI             // next 32 float32s (32*4 = 128 bytes)
	DECQ  CX
	JNZ   loop

done:
	// Horizontal sum of Y0 (8 floats → 1 float)
	VEXTRACTF128 $1, Y0, X1   // X1 = high 128 bits
	VADDPS X1, X0, X0         // X0 = low + high (4 floats)
	VHADDPS X0, X0, X0        // X0 = [a+b, c+d, a+b, c+d]
	VHADDPS X0, X0, X0        // X0 = [sum, sum, sum, sum]
	MOVSS X0, ret+64(FP)
	VZEROUPPER
	RET

// func dequantRowIntoAVX2(dst []float32, data []byte, rowOff, nBlocks int)
//
// Dequantizes a Q8_0 row into float32 destination using AVX2.
//
// Arguments:
//   dst.ptr    = +0(FP)
//   dst.len    = +8(FP)
//   dst.cap    = +16(FP)
//   data.ptr   = +24(FP)
//   data.len   = +32(FP)
//   data.cap   = +40(FP)
//   rowOff     = +48(FP)
//   nBlocks    = +56(FP)
TEXT ·dequantRowIntoAVX2(SB), NOSPLIT, $0-64
	MOVQ dst+0(FP), SI        // SI = &dst[0]
	MOVQ data+24(FP), DI      // DI = &data[0]
	MOVQ rowOff+48(FP), R8    // R8 = rowOff
	MOVQ nBlocks+56(FP), CX   // CX = nBlocks
	ADDQ R8, DI               // DI = &data[rowOff]

	TESTQ CX, CX
	JZ    dq_done

dq_loop:
	// Load and convert f16 scale (same as dotQ8RowAVX2)
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
	JZ    dq_scale_zero
	CMPL  R9, $31
	JE    dq_scale_inf
	ADDL  $112, R9
	SHLL  $23, R9
	SHLL  $13, R10
	ORL   R9, DX
	ORL   R10, DX
	JMP   dq_scale_ready

dq_scale_zero:
	JMP   dq_scale_ready

dq_scale_inf:
	ORL   $0x7f800000, DX
	SHLL  $13, R10
	ORL   R10, DX

dq_scale_ready:
	MOVL  DX, X1
	VBROADCASTSS X1, Y1

	// Group 0: bytes [2..9] → dst[0..7]
	VPMOVSXBD 2(DI), X2
	VPMOVSXBD 6(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, (SI)

	// Group 1: bytes [10..17] → dst[8..15]
	VPMOVSXBD 10(DI), X2
	VPMOVSXBD 14(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 32(SI)

	// Group 2: bytes [18..25] → dst[16..23]
	VPMOVSXBD 18(DI), X2
	VPMOVSXBD 22(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 64(SI)

	// Group 3: bytes [26..33] → dst[24..31]
	VPMOVSXBD 26(DI), X2
	VPMOVSXBD 30(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 96(SI)

	ADDQ  $34, DI
	ADDQ  $128, SI
	DECQ  CX
	JNZ   dq_loop

dq_done:
	VZEROUPPER
	RET

// func dequantRowScaledAVX2(dst []float32, data []byte, scales *float32, rowOff, nBlocks int)
//
// Like dequantRowIntoAVX2 but scale is preconverted f32 at scales[b].
// Frame: dst+0 (slice 24), data+24 (slice 24), scales+48, rowOff+56, nBlocks+64 = 72
TEXT ·dequantRowScaledAVX2(SB), NOSPLIT, $0-72
	MOVQ dst+0(FP), SI
	MOVQ data+24(FP), DI
	MOVQ scales+48(FP), R8    // R8 = &scales[0]
	MOVQ rowOff+56(FP), R9
	MOVQ nBlocks+64(FP), CX
	ADDQ R9, DI               // DI = &data[rowOff]

	TESTQ CX, CX
	JZ    dqs_done

	// Dual-block body when even count (K=512 → 16, K=2048 → 64)
	MOVQ CX, R10
	SHRQ $1, R10              // nBlocks/2
	TESTQ R10, R10
	JZ   dqs_odd

dqs_loop2:
	PREFETCHT0 68(DI)
	PREFETCHT0 8(R8)
	PREFETCHT0 256(SI)

	// ── block 0 ──
	MOVSS (R8), X1
	VBROADCASTSS X1, Y1
	VPMOVSXBD 2(DI), X2
	VPMOVSXBD 6(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, (SI)
	VPMOVSXBD 10(DI), X2
	VPMOVSXBD 14(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 32(SI)
	VPMOVSXBD 18(DI), X2
	VPMOVSXBD 22(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 64(SI)
	VPMOVSXBD 26(DI), X2
	VPMOVSXBD 30(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 96(SI)

	// ── block 1 (data+34, scale+4, dst+128) ──
	MOVSS 4(R8), X1
	VBROADCASTSS X1, Y1
	VPMOVSXBD 36(DI), X2
	VPMOVSXBD 40(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 128(SI)
	VPMOVSXBD 44(DI), X2
	VPMOVSXBD 48(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 160(SI)
	VPMOVSXBD 52(DI), X2
	VPMOVSXBD 56(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 192(SI)
	VPMOVSXBD 60(DI), X2
	VPMOVSXBD 64(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 224(SI)

	ADDQ $68, DI
	ADDQ $8, R8
	ADDQ $256, SI
	DECQ R10
	JNZ  dqs_loop2

	// remaining single block if odd
	MOVQ nBlocks+64(FP), CX
	ANDQ $1, CX
	JZ   dqs_done

dqs_odd:
	// fall through for 1 leftover or fully-odd nBlocks path
	TESTQ CX, CX
	JZ   dqs_done

dqs_loop1:
	PREFETCHT0 34(DI)
	MOVSS (R8), X1
	VBROADCASTSS X1, Y1
	ADDQ  $4, R8

	VPMOVSXBD 2(DI), X2
	VPMOVSXBD 6(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, (SI)

	VPMOVSXBD 10(DI), X2
	VPMOVSXBD 14(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 32(SI)

	VPMOVSXBD 18(DI), X2
	VPMOVSXBD 22(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 64(SI)

	VPMOVSXBD 26(DI), X2
	VPMOVSXBD 30(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS    Y1, Y2, Y2
	VMOVUPS   Y2, 96(SI)

	ADDQ  $34, DI
	ADDQ  $128, SI
	DECQ  CX
	JNZ   dqs_loop1

dqs_done:
	VZEROUPPER
	RET

// func dequantRowScaledDualAVX2(dst0, dst1 []float32, data []byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)
// Dequant two B rows in lockstep (shared block loop / better ILP for panel fill).
// Dual-block unroll when nBlocks even (encoder K=512 → 16 blocks).
// Frame: dst0+0(24) dst1+24(24) data+48(24) s0+72 s1+80 off0+88 off1+96 nBlocks+104 = 112
TEXT ·dequantRowScaledDualAVX2(SB), NOSPLIT, $0-112
	MOVQ dst0+0(FP), SI
	MOVQ dst1+24(FP), R10
	MOVQ data+48(FP), DI
	MOVQ scales0+72(FP), R8
	MOVQ scales1+80(FP), R9
	MOVQ rowOff0+88(FP), R11
	MOVQ data+48(FP), R15
	ADDQ R11, DI               // DI = data[row0]
	ADDQ rowOff1+96(FP), R15   // R15 = data[row1]
	MOVQ nBlocks+104(FP), CX
	TESTQ CX, CX
	JZ   dqsd_done

	// Prefer dual-block body when nBlocks even
	MOVQ CX, R11
	SHRQ $1, R11
	TESTQ R11, R11
	JZ   dqsd_one

dqsd_loop2:
	PREFETCHT0 68(DI)
	PREFETCHT0 68(R15)

	// ── block 0 ──
	MOVSS (R8), X1
	VBROADCASTSS X1, Y1
	MOVSS (R9), X4
	VBROADCASTSS X4, Y4

	VPMOVSXBD 2(DI), X2
	VPMOVSXBD 6(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, (SI)
	VPMOVSXBD 2(R15), X2
	VPMOVSXBD 6(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y4, Y2, Y2
	VMOVUPS Y2, (R10)

	VPMOVSXBD 10(DI), X2
	VPMOVSXBD 14(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 32(SI)
	VPMOVSXBD 10(R15), X2
	VPMOVSXBD 14(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y4, Y2, Y2
	VMOVUPS Y2, 32(R10)

	VPMOVSXBD 18(DI), X2
	VPMOVSXBD 22(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 64(SI)
	VPMOVSXBD 18(R15), X2
	VPMOVSXBD 22(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y4, Y2, Y2
	VMOVUPS Y2, 64(R10)

	VPMOVSXBD 26(DI), X2
	VPMOVSXBD 30(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 96(SI)
	VPMOVSXBD 26(R15), X2
	VPMOVSXBD 30(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y4, Y2, Y2
	VMOVUPS Y2, 96(R10)

	// ── block 1 ──
	MOVSS 4(R8), X1
	VBROADCASTSS X1, Y1
	MOVSS 4(R9), X4
	VBROADCASTSS X4, Y4

	VPMOVSXBD 36(DI), X2
	VPMOVSXBD 40(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 128(SI)
	VPMOVSXBD 36(R15), X2
	VPMOVSXBD 40(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y4, Y2, Y2
	VMOVUPS Y2, 128(R10)

	VPMOVSXBD 44(DI), X2
	VPMOVSXBD 48(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 160(SI)
	VPMOVSXBD 44(R15), X2
	VPMOVSXBD 48(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y4, Y2, Y2
	VMOVUPS Y2, 160(R10)

	VPMOVSXBD 52(DI), X2
	VPMOVSXBD 56(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 192(SI)
	VPMOVSXBD 52(R15), X2
	VPMOVSXBD 56(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y4, Y2, Y2
	VMOVUPS Y2, 192(R10)

	VPMOVSXBD 60(DI), X2
	VPMOVSXBD 64(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 224(SI)
	VPMOVSXBD 60(R15), X2
	VPMOVSXBD 64(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y4, Y2, Y2
	VMOVUPS Y2, 224(R10)

	ADDQ $8, R8
	ADDQ $8, R9
	ADDQ $68, DI
	ADDQ $68, R15
	ADDQ $256, SI
	ADDQ $256, R10
	DECQ R11
	JNZ  dqsd_loop2

	MOVQ nBlocks+104(FP), CX
	ANDQ $1, CX
	JZ   dqsd_done

dqsd_one:
	// CX = remaining single blocks (1 leftover or full nBlocks when nBlocks==1)
dqsd_loop:
	PREFETCHT0 34(DI)
	PREFETCHT0 34(R15)

	// row0 block
	MOVSS (R8), X1
	VBROADCASTSS X1, Y1
	ADDQ $4, R8
	VPMOVSXBD 2(DI), X2
	VPMOVSXBD 6(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, (SI)
	VPMOVSXBD 10(DI), X2
	VPMOVSXBD 14(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 32(SI)
	VPMOVSXBD 18(DI), X2
	VPMOVSXBD 22(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 64(SI)
	VPMOVSXBD 26(DI), X2
	VPMOVSXBD 30(DI), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 96(SI)

	// row1 block
	MOVSS (R9), X1
	VBROADCASTSS X1, Y1
	ADDQ $4, R9
	VPMOVSXBD 2(R15), X2
	VPMOVSXBD 6(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, (R10)
	VPMOVSXBD 10(R15), X2
	VPMOVSXBD 14(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 32(R10)
	VPMOVSXBD 18(R15), X2
	VPMOVSXBD 22(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 64(R10)
	VPMOVSXBD 26(R15), X2
	VPMOVSXBD 30(R15), X3
	VINSERTI128 $1, X3, Y2, Y2
	VCVTDQ2PS Y2, Y2
	VMULPS Y1, Y2, Y2
	VMOVUPS Y2, 96(R10)

	ADDQ $34, DI
	ADDQ $34, R15
	ADDQ $128, SI
	ADDQ $128, R10
	DECQ CX
	JNZ  dqsd_loop

dqsd_done:
	VZEROUPPER
	RET
