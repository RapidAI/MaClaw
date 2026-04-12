//go:build amd64

#include "textflag.h"

// Schraudolph fast exp:
//   exp(x) ≈ reinterpret_as_float32(int32(A * x) + B)
//   A = 2^23 / ln(2) = 12102203.16
//   B = 127 * 2^23 - 60801 = 1065292415
//
// Strategy: compute A*x in float, truncate to int32, add B as integer,
// then reinterpret the int32 bits as float32.
// This avoids float32 precision loss when representing B.

// func siluMulAVX2(gate, up []float32)
TEXT ·siluMulAVX2(SB), NOSPLIT, $0-48
	MOVQ gate_base+0(FP), SI       // SI = &gate[0]
	MOVQ gate_len+8(FP), CX       // CX = len(gate)
	MOVQ up_base+24(FP), DI        // DI = &up[0]

	TESTQ CX, CX
	JZ    silu_done

	// Vector constants
	// Y4 = A = 12102203.0
	MOVL  $0x4B38AA3B, AX
	MOVL  AX, X4
	VBROADCASTSS X4, Y4

	// Y5 = B = 1065292415 as int32 (for VPADDD)
	MOVL  $0x3F7F127F, AX     // 1065292415 = 0x3F7F127F
	MOVL  AX, X5
	VPBROADCASTD X5, Y5

	// Y6 = -88.0
	MOVL  $0xC2B00000, AX
	MOVL  AX, X6
	VBROADCASTSS X6, Y6

	// Y7 = 88.0
	MOVL  $0x42B00000, AX
	MOVL  AX, X7
	VBROADCASTSS X7, Y7

	// Y8 = 1.0
	MOVL  $0x3F800000, AX
	MOVL  AX, X8
	VBROADCASTSS X8, Y8

	// Y9 = 0.0
	VXORPS Y9, Y9, Y9

	// Process 8 at a time
	MOVQ  CX, R8
	SHRQ  $3, R8
	TESTQ R8, R8
	JZ    silu_tail

silu_loop8:
	VMOVUPS (SI), Y0          // Y0 = v

	// -v, clamped to [-88, 88]
	VSUBPS  Y0, Y9, Y1       // Y1 = -v
	VMAXPS  Y6, Y1, Y1
	VMINPS  Y7, Y1, Y1

	// Fast exp(-v):
	// step 1: float_part = A * (-v)
	VMULPS  Y4, Y1, Y2       // Y2 = A * (-v)
	// step 2: int_part = truncate to int32
	VCVTTPS2DQ Y2, Y2        // Y2 = int32(A * (-v))
	// step 3: add B as integer
	VPADDD  Y5, Y2, Y2       // Y2 = int32(A*(-v)) + B
	// Y2 now holds bit patterns that represent exp(-v) when read as float32

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
	JNZ   silu_loop8

silu_tail:
	ANDQ  $7, CX
	TESTQ CX, CX
	JZ    silu_done

	// Scalar tail — use the same int-add approach
	MOVL  $0x4B38AA3B, AX     // A
	MOVL  AX, X4
	MOVL  $0x3F7F127F, R9     // B as int32

silu_scalar:
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
	JNZ    silu_scalar

silu_done:
	VZEROUPPER
	RET
