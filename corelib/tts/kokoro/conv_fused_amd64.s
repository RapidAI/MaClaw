//go:build amd64

#include "textflag.h"

// func dot3AVX2(a0, a1, a2, w0, w1, w2 []float32) float32
// Computes sum(a0*w0 + a1*w1 + a2*w2). All slices must have the same length.
TEXT ·dot3AVX2(SB), NOSPLIT, $0-148
	MOVQ a0_base+0(FP), SI
	MOVQ a0_len+8(FP), CX
	MOVQ a1_base+24(FP), R8
	MOVQ a2_base+48(FP), R9
	MOVQ w0_base+72(FP), DI
	MOVQ w1_base+96(FP), R10
	MOVQ w2_base+120(FP), R11

	VXORPS Y0, Y0, Y0
	VXORPS X7, X7, X7
	TESTQ  CX, CX
	JZ     dot3_reduce

	MOVQ CX, AX
	SHRQ $3, AX
	TESTQ AX, AX
	JZ    dot3_tail

dot3_loop8:
	VMOVUPS (SI), Y1
	VMOVUPS (DI), Y2
	VMULPS  Y2, Y1, Y1

	VMOVUPS (R8), Y3
	VMOVUPS (R10), Y4
	VMULPS  Y4, Y3, Y3
	VADDPS  Y3, Y1, Y1

	VMOVUPS (R9), Y5
	VMOVUPS (R11), Y6
	VMULPS  Y6, Y5, Y5
	VADDPS  Y5, Y1, Y1

	VADDPS Y1, Y0, Y0

	ADDQ $32, SI
	ADDQ $32, R8
	ADDQ $32, R9
	ADDQ $32, DI
	ADDQ $32, R10
	ADDQ $32, R11
	DECQ AX
	JNZ  dot3_loop8

dot3_tail:
	ANDQ $7, CX
	TESTQ CX, CX
	JZ    dot3_reduce

dot3_scalar:
	VMOVSS (SI), X1
	VMOVSS (DI), X2
	VMULSS X2, X1, X1

	VMOVSS (R8), X3
	VMOVSS (R10), X4
	VMULSS X4, X3, X3
	VADDSS X3, X1, X1

	VMOVSS (R9), X5
	VMOVSS (R11), X6
	VMULSS X6, X5, X5
	VADDSS X5, X1, X1

	VADDSS X1, X7, X7

	ADDQ $4, SI
	ADDQ $4, R8
	ADDQ $4, R9
	ADDQ $4, DI
	ADDQ $4, R10
	ADDQ $4, R11
	DECQ CX
	JNZ  dot3_scalar

dot3_reduce:
	VEXTRACTF128 $1, Y0, X1
	VADDPS X1, X0, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0
	VADDSS X7, X0, X0
	MOVSS X0, ret+144(FP)
	VZEROUPPER
	RET

// func dot3FMA(a0, a1, a2, w0, w1, w2 []float32) float32
// Same operation as dot3AVX2, using FMA when the CPU advertises AVX2+FMA.
TEXT ·dot3FMA(SB), NOSPLIT, $0-148
	MOVQ a0_base+0(FP), SI
	MOVQ a0_len+8(FP), CX
	MOVQ a1_base+24(FP), R8
	MOVQ a2_base+48(FP), R9
	MOVQ w0_base+72(FP), DI
	MOVQ w1_base+96(FP), R10
	MOVQ w2_base+120(FP), R11

	VXORPS Y0, Y0, Y0
	VXORPS X7, X7, X7
	TESTQ  CX, CX
	JZ     dot3_fma_reduce

	MOVQ CX, AX
	SHRQ $3, AX
	TESTQ AX, AX
	JZ    dot3_fma_tail

dot3_fma_loop8:
	VMOVUPS (SI), Y1
	VMOVUPS (DI), Y2
	VFMADD231PS Y2, Y1, Y0

	VMOVUPS (R8), Y3
	VMOVUPS (R10), Y4
	VFMADD231PS Y4, Y3, Y0

	VMOVUPS (R9), Y5
	VMOVUPS (R11), Y6
	VFMADD231PS Y6, Y5, Y0

	ADDQ $32, SI
	ADDQ $32, R8
	ADDQ $32, R9
	ADDQ $32, DI
	ADDQ $32, R10
	ADDQ $32, R11
	DECQ AX
	JNZ  dot3_fma_loop8

dot3_fma_tail:
	ANDQ $7, CX
	TESTQ CX, CX
	JZ    dot3_fma_reduce

dot3_fma_scalar:
	VMOVSS (SI), X1
	VMOVSS (DI), X2
	VFMADD231SS X2, X1, X7

	VMOVSS (R8), X3
	VMOVSS (R10), X4
	VFMADD231SS X4, X3, X7

	VMOVSS (R9), X5
	VMOVSS (R11), X6
	VFMADD231SS X6, X5, X7

	ADDQ $4, SI
	ADDQ $4, R8
	ADDQ $4, R9
	ADDQ $4, DI
	ADDQ $4, R10
	ADDQ $4, R11
	DECQ CX
	JNZ  dot3_fma_scalar

dot3_fma_reduce:
	VEXTRACTF128 $1, Y0, X1
	VADDPS X1, X0, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0
	VADDSS X7, X0, X0
	MOVSS X0, ret+144(FP)
	VZEROUPPER
	RET
