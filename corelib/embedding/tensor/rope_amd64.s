//go:build amd64

#include "textflag.h"

// func ropePrecomputedAVX(x []float32, nHeads, headDim int, cosTable, sinTable []float32)
//
// Applies Rotary Position Embedding using pre-computed cos/sin tables.
// For each head h in [0, nHeads):
//   for i in [0, halfDim):
//     x[h*headDim + i]          = x0*cos[i] - x1*sin[i]
//     x[h*headDim + i + halfDim] = x0*sin[i] + x1*cos[i]
// where x0 = x[h*headDim+i], x1 = x[h*headDim+i+halfDim]
//
// Uses AVX1 to process 8 pairs per iteration (8 cos/sin values at a time).
// The tricky part: x0 and x1 are halfDim apart, so we load from two
// non-contiguous regions and store back to the same two regions.
//
// Arguments:
//   x.ptr       = +0(FP)
//   x.len       = +8(FP)
//   x.cap       = +16(FP)
//   nHeads      = +24(FP)
//   headDim     = +32(FP)
//   cosTable.ptr = +40(FP)
//   cosTable.len = +48(FP)
//   cosTable.cap = +56(FP)
//   sinTable.ptr = +64(FP)
//   sinTable.len = +72(FP)
//   sinTable.cap = +80(FP)
TEXT ·ropePrecomputedAVX(SB), NOSPLIT, $0-88
	MOVQ x+0(FP), SI          // SI = &x[0]
	MOVQ nHeads+24(FP), R8    // R8 = nHeads
	MOVQ headDim+32(FP), R9   // R9 = headDim
	MOVQ cosTable+40(FP), R10 // R10 = &cosTable[0]
	MOVQ sinTable+64(FP), R11 // R11 = &sinTable[0]

	MOVQ  R9, R12
	SHRQ  $1, R12             // R12 = halfDim = headDim / 2

	// headDimBytes = headDim * 4 (byte stride per head)
	MOVQ  R9, R13
	SHLQ  $2, R13             // R13 = headDim * 4

	// halfDimBytes = halfDim * 4 (byte offset to second half)
	MOVQ  R12, R14
	SHLQ  $2, R14             // R14 = halfDim * 4

	TESTQ R8, R8
	JZ    rope_done

	MOVQ  SI, BX              // BX = current head pointer (will advance)

rope_head_loop:
	// Process one head: BX points to x[h*headDim]
	// x0 region: BX[0 .. halfDim*4)
	// x1 region: BX[halfDim*4 .. headDim*4)

	MOVQ  R12, CX             // CX = halfDim (elements to process)
	XORQ  DX, DX              // DX = byte offset within head (i * 4)

	// Process 8 elements at a time
	MOVQ  CX, AX
	SHRQ  $3, AX              // AX = halfDim / 8
	TESTQ AX, AX
	JZ    rope_tail

rope_inner8:
	// Load cos[i..i+7] and sin[i..i+7]
	VMOVUPS (R10)(DX*1), Y0   // Y0 = cos[i..i+7]
	VMOVUPS (R11)(DX*1), Y1   // Y1 = sin[i..i+7]

	// Load x0[i..i+7] from first half
	VMOVUPS (BX)(DX*1), Y2    // Y2 = x0

	// Load x1[i..i+7] from second half (offset by halfDim*4)
	LEAQ    (BX)(R14*1), R15
	VMOVUPS (R15)(DX*1), Y3   // Y3 = x1

	// new_x0 = x0 * cos - x1 * sin
	VMULPS  Y0, Y2, Y4        // Y4 = x0 * cos
	VMULPS  Y1, Y3, Y5        // Y5 = x1 * sin
	VSUBPS  Y5, Y4, Y4        // Y4 = x0*cos - x1*sin

	// new_x1 = x0 * sin + x1 * cos
	VMULPS  Y1, Y2, Y5        // Y5 = x0 * sin
	VMULPS  Y0, Y3, Y6        // Y6 = x1 * cos
	VADDPS  Y6, Y5, Y5        // Y5 = x0*sin + x1*cos

	// Store results
	VMOVUPS Y4, (BX)(DX*1)    // store new x0
	VMOVUPS Y5, (R15)(DX*1)   // store new x1

	ADDQ  $32, DX             // advance by 8 floats (32 bytes)
	DECQ  AX
	JNZ   rope_inner8

rope_tail:
	// Handle remaining elements (0-7) with scalar
	MOVQ  R14, AX             // total bytes for halfDim
	SUBQ  DX, AX              // remaining bytes
	SHRQ  $2, AX              // remaining elements
	TESTQ AX, AX
	JZ    rope_next_head

rope_scalar:
	VMOVSS (R10)(DX*1), X0    // cos[i]
	VMOVSS (R11)(DX*1), X1    // sin[i]
	VMOVSS (BX)(DX*1), X2     // x0
	LEAQ   (BX)(R14*1), R15
	VMOVSS (R15)(DX*1), X3    // x1

	// new_x0 = x0*cos - x1*sin
	VMULSS X0, X2, X4
	VMULSS X1, X3, X5
	VSUBSS X5, X4, X4
	VMOVSS X4, (BX)(DX*1)

	// new_x1 = x0*sin + x1*cos
	VMULSS X1, X2, X5
	VMULSS X0, X3, X6
	VADDSS X6, X5, X5
	VMOVSS X5, (R15)(DX*1)

	ADDQ  $4, DX
	DECQ  AX
	JNZ   rope_scalar

rope_next_head:
	ADDQ  R13, BX             // advance to next head
	DECQ  R8
	JNZ   rope_head_loop

rope_done:
	VZEROUPPER
	RET
