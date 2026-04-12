//go:build amd64

#include "textflag.h"

// func siluMulAVX1(gate, up []float32)
//
// AVX1-only SiLU(gate)*up. Same Schraudolph fast-exp trick but without
// AVX2-only instructions. All broadcasts use PSHUFD+VINSERTF128 instead
// of VBROADCASTSS xmm,ymm (which requires AVX2 for register source).
//
TEXT ·siluMulAVX1(SB), NOSPLIT, $0-48
	MOVQ gate_base+0(FP), SI
	MOVQ gate_len+8(FP), CX
	MOVQ up_base+24(FP), DI

	TESTQ CX, CX
	JZ    avx1_silu_done

	// Float constants via PSHUFD+VINSERTF128 (AVX1-safe broadcast)
	// Y6 = -88.0
	MOVL  $0xC2B00000, AX
	MOVL  AX, X6
	PSHUFD $0, X6, X6
	VINSERTF128 $1, X6, Y6, Y6

	// Y7 = 88.0
	MOVL  $0x42B00000, AX
	MOVL  AX, X7
	PSHUFD $0, X7, X7
	VINSERTF128 $1, X7, Y7, Y7

	// Y8 = 1.0
	MOVL  $0x3F800000, AX
	MOVL  AX, X8
	PSHUFD $0, X8, X8
	VINSERTF128 $1, X8, Y8, Y8

	// Y9 = 0.0
	VXORPS Y9, Y9, Y9

	// Y4 = A = 12102203.0 (float for VMULPS)
	MOVL  $0x4B38AA3B, AX
	MOVL  AX, X4
	PSHUFD $0, X4, X4
	VINSERTF128 $1, X4, Y4, Y4

	// Y5 = B = 1065292415 as int32 (for PADDD)
	MOVL  $0x3F7F127F, AX
	MOVD  AX, X5
	PSHUFD $0, X5, X5
	VINSERTF128 $1, X5, Y5, Y5

	// Process 8 at a time
	MOVQ  CX, R8
	SHRQ  $3, R8
	TESTQ R8, R8
	JZ    avx1_silu_tail

avx1_silu_loop8:
	VMOVUPS (SI), Y0          // Y0 = v

	// -v, clamped
	VSUBPS  Y0, Y9, Y1       // Y1 = -v
	VMAXPS  Y6, Y1, Y1
	VMINPS  Y7, Y1, Y1

	// Fast exp(-v): A * (-v), truncate to int32, add B
	VMULPS  Y4, Y1, Y2       // Y2 = A * (-v)
	VCVTTPS2DQ Y2, Y2        // Y2 = int32(A * (-v))
	// AVX1 has no VPADDD. Split into two 128-bit halves.
	VEXTRACTF128 $1, Y2, X3  // X3 = high half
	VEXTRACTF128 $1, Y5, X10 // X10 = B high half
	PADDD X10, X3             // X3 += B (high)
	PADDD X5, X2              // X2 += B (low, X2 is low 128 of Y2)
	VINSERTF128 $1, X3, Y2, Y2 // Y2 = reassembled [exp(-v) bits]

	// SiLU: v / (1 + exp(-v))
	VADDPS  Y8, Y2, Y3       // Y3 = 1.0 + exp(-v)
	VDIVPS  Y3, Y0, Y0       // Y0 = v / (1 + exp(-v))

	// * up
	VMOVUPS (DI), Y1
	VMULPS  Y1, Y0, Y0
	VMOVUPS Y0, (SI)

	ADDQ  $32, SI
	ADDQ  $32, DI
	DECQ  R8
	JNZ   avx1_silu_loop8

avx1_silu_tail:
	ANDQ  $7, CX
	TESTQ CX, CX
	JZ    avx1_silu_done

	// Scalar tail — uses VEX scalar ops (AVX1 safe)
	MOVL  $0x4B38AA3B, AX     // A
	MOVL  AX, X4
	MOVL  $0x3F7F127F, R9     // B as int32

avx1_silu_scalar:
	VMOVSS  (SI), X0          // v
	VSUBSS  X0, X9, X1        // -v
	VMAXSS  X6, X1, X1
	VMINSS  X7, X1, X1
	VMULSS  X4, X1, X2        // A * (-v)
	VCVTTSS2SI X2, AX         // int32(A * (-v))
	ADDL   R9, AX             // + B
	MOVL   AX, X2             // reinterpret as float32
	VADDSS  X8, X2, X3        // 1 + exp(-v)
	VDIVSS  X3, X0, X0        // SiLU
	VMOVSS  (DI), X1
	VMULSS  X1, X0, X0
	VMOVSS  X0, (SI)
	ADDQ   $4, SI
	ADDQ   $4, DI
	DECQ   CX
	JNZ    avx1_silu_scalar

avx1_silu_done:
	VZEROUPPER
	RET
