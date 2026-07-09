//go:build amd64

#include "textflag.h"

// func multiDot4AVX2(out *[4]float32, a, b *float32, K int)
//
// out[i] = sum_k a[i*K + k] * b[k]  for i in 0..3
// a is contiguous [4][K], b is [K].
//
// Frame:
//   out  +0
//   a    +8
//   b    +16
//   K    +24
TEXT ·multiDot4AVX2(SB), NOSPLIT, $0-32
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b+16(FP), DI
	MOVQ K+24(FP), CX

	// Bytes per row = K * 4
	MOVQ CX, AX
	SHLQ $2, AX

	// Row bases
	MOVQ SI, R8
	LEAQ (SI)(AX*1), R9
	LEAQ (R9)(AX*1), R10
	LEAQ (R10)(AX*1), DX

	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3

	MOVQ CX, R12
	SHRQ $3, R12
	TESTQ R12, R12
	JZ   hsum

loop8:
	VMOVUPS (DI), Y4

	VMOVUPS (R8), Y5
	VFMADD231PS Y5, Y4, Y0

	VMOVUPS (R9), Y5
	VFMADD231PS Y5, Y4, Y1

	VMOVUPS (R10), Y5
	VFMADD231PS Y5, Y4, Y2

	VMOVUPS (DX), Y5
	VFMADD231PS Y5, Y4, Y3

	ADDQ $32, DI
	ADDQ $32, R8
	ADDQ $32, R9
	ADDQ $32, R10
	ADDQ $32, DX

	DECQ R12
	JNZ  loop8

hsum:
	VEXTRACTF128 $1, Y0, X4
	VADDPS X0, X4, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0
	MOVSS X0, (R11)

	VEXTRACTF128 $1, Y1, X4
	VADDPS X1, X4, X1
	VHADDPS X1, X1, X1
	VHADDPS X1, X1, X1
	MOVSS X1, 4(R11)

	VEXTRACTF128 $1, Y2, X4
	VADDPS X2, X4, X2
	VHADDPS X2, X2, X2
	VHADDPS X2, X2, X2
	MOVSS X2, 8(R11)

	VEXTRACTF128 $1, Y3, X4
	VADDPS X3, X4, X3
	VHADDPS X3, X3, X3
	VHADDPS X3, X3, X3
	MOVSS X3, 12(R11)

	// Scalar tail K%8
	MOVQ K+24(FP), CX
	MOVQ CX, R12
	ANDQ $7, R12
	JZ   done

	// Reload bases, skip (K&^7) elements
	MOVQ a+8(FP), SI
	MOVQ b+16(FP), DI
	MOVQ CX, AX
	SHLQ $2, AX               // row stride bytes
	MOVQ CX, R13
	ANDQ $~7, R13
	SHLQ $2, R13              // byte offset into each row

	ADDQ R13, DI

	MOVQ SI, R8
	ADDQ R13, R8
	LEAQ (SI)(AX*1), R9
	ADDQ R13, R9
	LEAQ (SI)(AX*2), R10
	ADDQ R13, R10
	// row3 = SI + 3*AX
	MOVQ AX, DX
	ADDQ AX, DX
	ADDQ AX, DX
	ADDQ SI, DX
	ADDQ R13, DX

tail:
	MOVSS (DI), X4
	MOVSS (R8), X5
	MULSS X4, X5
	ADDSS (R11), X5
	MOVSS X5, (R11)

	MOVSS (R9), X5
	MULSS X4, X5
	ADDSS 4(R11), X5
	MOVSS X5, 4(R11)

	MOVSS (R10), X5
	MULSS X4, X5
	ADDSS 8(R11), X5
	MOVSS X5, 8(R11)

	MOVSS (DX), X5
	MULSS X4, X5
	ADDSS 12(R11), X5
	MOVSS X5, 12(R11)

	ADDQ $4, DI
	ADDQ $4, R8
	ADDQ $4, R9
	ADDQ $4, R10
	ADDQ $4, DX
	DECQ R12
	JNZ  tail

done:
	VZEROUPPER
	RET

// func multiDot8AVX2(out *[8]float32, a, b *float32, K int)
// Same as multiDot4 but 8 A rows — B loaded once per chunk into 8 FMA chains.
// Frame: out+0, a+8, b+16, K+24
TEXT ·multiDot8AVX2(SB), NOSPLIT, $0-32
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b+16(FP), DI
	MOVQ K+24(FP), CX

	MOVQ CX, AX
	SHLQ $2, AX               // row stride bytes

	// Row pointers R8,R9,R10,DX,R12,R13,R14,R15 — only 8 general regs available carefully
	// Use stack for extra row pointers (NOSPLIT $64)
	// Simpler: call multiDot4AVX2 twice from Go — already done in multiDot8 fallback.
	// True 8-wide: keep 8 accums Y0-Y7, compute row bases from SI+i*AX

	// Zero accumulators Y0..Y7
	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3
	VXORPS Y4, Y4, Y4
	VXORPS Y5, Y5, Y5
	VXORPS Y6, Y6, Y6
	VXORPS Y7, Y7, Y7

	// Save SI, AX, DI; loop with k index
	// R8 = k byte offset (0..K*4)
	XORQ R8, R8
	MOVQ CX, R9
	SHRQ $3, R9               // blocks of 8
	TESTQ R9, R9
	JZ   hsum8

loop8_8:
	// B chunk
	VMOVUPS (DI)(R8*1), Y8

	// row i at SI + i*AX + R8
	// row0
	VMOVUPS (SI)(R8*1), Y9
	VFMADD231PS Y9, Y8, Y0
	// row1
	MOVQ SI, R10
	ADDQ AX, R10
	VMOVUPS (R10)(R8*1), Y9
	VFMADD231PS Y9, Y8, Y1
	// row2
	ADDQ AX, R10
	VMOVUPS (R10)(R8*1), Y9
	VFMADD231PS Y9, Y8, Y2
	// row3
	ADDQ AX, R10
	VMOVUPS (R10)(R8*1), Y9
	VFMADD231PS Y9, Y8, Y3
	// row4
	ADDQ AX, R10
	VMOVUPS (R10)(R8*1), Y9
	VFMADD231PS Y9, Y8, Y4
	// row5
	ADDQ AX, R10
	VMOVUPS (R10)(R8*1), Y9
	VFMADD231PS Y9, Y8, Y5
	// row6
	ADDQ AX, R10
	VMOVUPS (R10)(R8*1), Y9
	VFMADD231PS Y9, Y8, Y6
	// row7
	ADDQ AX, R10
	VMOVUPS (R10)(R8*1), Y9
	VFMADD231PS Y9, Y8, Y7

	ADDQ $32, R8
	DECQ R9
	JNZ  loop8_8

hsum8:
	// Helper macro-like hsum for each Y into out[i]
	// Y0
	VEXTRACTF128 $1, Y0, X8
	VADDPS X0, X8, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0
	MOVSS X0, (R11)
	// Y1
	VEXTRACTF128 $1, Y1, X8
	VADDPS X1, X8, X1
	VHADDPS X1, X1, X1
	VHADDPS X1, X1, X1
	MOVSS X1, 4(R11)
	// Y2
	VEXTRACTF128 $1, Y2, X8
	VADDPS X2, X8, X2
	VHADDPS X2, X2, X2
	VHADDPS X2, X2, X2
	MOVSS X2, 8(R11)
	// Y3
	VEXTRACTF128 $1, Y3, X8
	VADDPS X3, X8, X3
	VHADDPS X3, X3, X3
	VHADDPS X3, X3, X3
	MOVSS X3, 12(R11)
	// Y4
	VEXTRACTF128 $1, Y4, X8
	VADDPS X4, X8, X4
	VHADDPS X4, X4, X4
	VHADDPS X4, X4, X4
	MOVSS X4, 16(R11)
	// Y5
	VEXTRACTF128 $1, Y5, X8
	VADDPS X5, X8, X5
	VHADDPS X5, X5, X5
	VHADDPS X5, X5, X5
	MOVSS X5, 20(R11)
	// Y6
	VEXTRACTF128 $1, Y6, X8
	VADDPS X6, X8, X6
	VHADDPS X6, X6, X6
	VHADDPS X6, X6, X6
	MOVSS X6, 24(R11)
	// Y7
	VEXTRACTF128 $1, Y7, X8
	VADDPS X7, X8, X7
	VHADDPS X7, X7, X7
	VHADDPS X7, X7, X7
	MOVSS X7, 28(R11)

	// Scalar tail K%8
	MOVQ K+24(FP), CX
	MOVQ CX, R9
	ANDQ $7, R9
	JZ   done8

	MOVQ a+8(FP), SI
	MOVQ b+16(FP), DI
	MOVQ CX, AX
	SHLQ $2, AX
	MOVQ CX, R8
	ANDQ $~7, R8
	SHLQ $2, R8               // byte offset
	ADDQ R8, DI

	// For each remaining k, accumulate 8 rows
tail8:
	MOVSS (DI), X8            // b
	// i=0..7
	XORQ R10, R10             // row index 0..7
	MOVQ SI, R12
	ADDQ R8, R12              // row0 + offset
row_tail:
	MOVSS (R12), X9
	MULSS X8, X9
	// out[R10] += 
	MOVSS (R11)(R10*4), X10
	ADDSS X9, X10
	MOVSS X10, (R11)(R10*4)
	ADDQ AX, R12              // next row
	INCQ R10
	CMPQ R10, $8
	JL   row_tail

	ADDQ $4, DI
	ADDQ $4, R8
	DECQ R9
	JNZ  tail8

done8:
	VZEROUPPER
	RET

// func multiDot4DualBAVX2(out *[8]float32, a, b0, b1 *float32, K int)
//
// out[0:4] = multiDot4(a, b0); out[4:8] = multiDot4(a, b1)
// Loads each A chunk once and FMAs into both B accumulator chains.
// Frame: out+0, a+8, b0+16, b1+24, K+32
TEXT ·multiDot4DualBAVX2(SB), NOSPLIT, $0-40
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
	MOVQ K+32(FP), CX

	// Row stride bytes = K*4
	MOVQ CX, AX
	SHLQ $2, AX

	// Row bases R8,R9,R10,R12
	MOVQ SI, R8
	LEAQ (SI)(AX*1), R9
	LEAQ (R9)(AX*1), R10
	LEAQ (R10)(AX*1), R12

	// Y0-Y3: dots with b0; Y4-Y7: dots with b1
	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3
	VXORPS Y4, Y4, Y4
	VXORPS Y5, Y5, Y5
	VXORPS Y6, Y6, Y6
	VXORPS Y7, Y7, Y7

	MOVQ CX, R13
	SHRQ $3, R13               // K/8
	TESTQ R13, R13
	JZ   dual_hsum

dual_loop:
	VMOVUPS (DI), Y8           // b0 chunk
	VMOVUPS (R15), Y9          // b1 chunk

	// row0
	VMOVUPS (R8), Y10
	VFMADD231PS Y10, Y8, Y0
	VFMADD231PS Y10, Y9, Y4
	// row1
	VMOVUPS (R9), Y10
	VFMADD231PS Y10, Y8, Y1
	VFMADD231PS Y10, Y9, Y5
	// row2
	VMOVUPS (R10), Y10
	VFMADD231PS Y10, Y8, Y2
	VFMADD231PS Y10, Y9, Y6
	// row3
	VMOVUPS (R12), Y10
	VFMADD231PS Y10, Y8, Y3
	VFMADD231PS Y10, Y9, Y7

	ADDQ $32, DI
	ADDQ $32, R15
	ADDQ $32, R8
	ADDQ $32, R9
	ADDQ $32, R10
	ADDQ $32, R12
	DECQ R13
	JNZ  dual_loop

dual_hsum:
	// hsum Y0..Y7 → out[0..7]
	VEXTRACTF128 $1, Y0, X8
	VADDPS X0, X8, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0
	MOVSS X0, (R11)

	VEXTRACTF128 $1, Y1, X8
	VADDPS X1, X8, X1
	VHADDPS X1, X1, X1
	VHADDPS X1, X1, X1
	MOVSS X1, 4(R11)

	VEXTRACTF128 $1, Y2, X8
	VADDPS X2, X8, X2
	VHADDPS X2, X2, X2
	VHADDPS X2, X2, X2
	MOVSS X2, 8(R11)

	VEXTRACTF128 $1, Y3, X8
	VADDPS X3, X8, X3
	VHADDPS X3, X3, X3
	VHADDPS X3, X3, X3
	MOVSS X3, 12(R11)

	VEXTRACTF128 $1, Y4, X8
	VADDPS X4, X8, X4
	VHADDPS X4, X4, X4
	VHADDPS X4, X4, X4
	MOVSS X4, 16(R11)

	VEXTRACTF128 $1, Y5, X8
	VADDPS X5, X8, X5
	VHADDPS X5, X5, X5
	VHADDPS X5, X5, X5
	MOVSS X5, 20(R11)

	VEXTRACTF128 $1, Y6, X8
	VADDPS X6, X8, X6
	VHADDPS X6, X6, X6
	VHADDPS X6, X6, X6
	MOVSS X6, 24(R11)

	VEXTRACTF128 $1, Y7, X8
	VADDPS X7, X8, X7
	VHADDPS X7, X7, X7
	VHADDPS X7, X7, X7
	MOVSS X7, 28(R11)

	// Scalar tail K%8 — pointers already advanced past main loop
	MOVQ K+32(FP), CX
	MOVQ CX, R13
	ANDQ $7, R13
	JZ   dual_done

dual_tail:
	MOVSS (DI), X8             // b0
	MOVSS (R15), X9            // b1

	MOVSS (R8), X10
	MULSS X8, X10
	ADDSS (R11), X10
	MOVSS X10, (R11)
	MOVSS (R8), X10
	MULSS X9, X10
	ADDSS 16(R11), X10
	MOVSS X10, 16(R11)

	MOVSS (R9), X10
	MULSS X8, X10
	ADDSS 4(R11), X10
	MOVSS X10, 4(R11)
	MOVSS (R9), X10
	MULSS X9, X10
	ADDSS 20(R11), X10
	MOVSS X10, 20(R11)

	MOVSS (R10), X10
	MULSS X8, X10
	ADDSS 8(R11), X10
	MOVSS X10, 8(R11)
	MOVSS (R10), X10
	MULSS X9, X10
	ADDSS 24(R11), X10
	MOVSS X10, 24(R11)

	MOVSS (R12), X10
	MULSS X8, X10
	ADDSS 12(R11), X10
	MOVSS X10, 12(R11)
	MOVSS (R12), X10
	MULSS X9, X10
	ADDSS 28(R11), X10
	MOVSS X10, 28(R11)

	ADDQ $4, DI
	ADDQ $4, R15
	ADDQ $4, R8
	ADDQ $4, R9
	ADDQ $4, R10
	ADDQ $4, R12
	DECQ R13
	JNZ  dual_tail

dual_done:
	VZEROUPPER
	RET
