//go:build amd64

#include "textflag.h"

// func dotQ8RowASM(a []float32, data []byte, rowOff, nBlocks int) float32
//
// Computes dot product of float32 vector a[] with a Q8_0 row.
// Q8_0 block layout: [scale:f16(2 bytes)][d0..d31:int8(32 bytes)] = 34 bytes.
// Uses AVX2: vpmovsxbd (int8→int32), vcvtdq2ps (int32→f32), vfmadd231ps.
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
TEXT ·dotQ8RowASM(SB), NOSPLIT, $0-72
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

// func dequantRowIntoASM(dst []float32, data []byte, rowOff, nBlocks int)
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
TEXT ·dequantRowIntoASM(SB), NOSPLIT, $0-64
	MOVQ dst+0(FP), SI        // SI = &dst[0]
	MOVQ data+24(FP), DI      // DI = &data[0]
	MOVQ rowOff+48(FP), R8    // R8 = rowOff
	MOVQ nBlocks+56(FP), CX   // CX = nBlocks
	ADDQ R8, DI               // DI = &data[rowOff]

	TESTQ CX, CX
	JZ    dq_done

dq_loop:
	// Load and convert f16 scale (same as dotQ8RowASM)
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
