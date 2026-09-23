//go:build amd64

#include "textflag.h"

DATA attnExpConsts<>+0(SB)/4, $0x80000000 // signMask
DATA attnExpConsts<>+4(SB)/4, $0xc2b0c0a5 // expMin = -88.37626
DATA attnExpConsts<>+8(SB)/4, $0x42b0c0a5 // expMax = 88.37626
DATA attnExpConsts<>+12(SB)/4, $0x3fb8aa3b // log2e
DATA attnExpConsts<>+16(SB)/4, $0x3f000000 // half
DATA attnExpConsts<>+20(SB)/4, $0x3f318000 // expC1 = +0.693359375 (ln2 hi, subtracted)
DATA attnExpConsts<>+24(SB)/4, $0xb95e8083 // expC2 = -2.1219444e-4
DATA attnExpConsts<>+28(SB)/4, $0x39506967 // expP0
DATA attnExpConsts<>+32(SB)/4, $0x3ab743ce // expP1
DATA attnExpConsts<>+36(SB)/4, $0x3c088908 // expP2
DATA attnExpConsts<>+40(SB)/4, $0x3d2aa9c1 // expP3
DATA attnExpConsts<>+44(SB)/4, $0x3e2aaaaa // expP4
DATA attnExpConsts<>+48(SB)/4, $0x3f000000 // expP5
DATA attnExpConsts<>+52(SB)/4, $0x3f800000 // one
DATA attnExpConsts<>+56(SB)/4, $0x40000000 // two
DATA attnExpConsts<>+60(SB)/4, $0x0000007f // bias127
GLOBL attnExpConsts<>(SB), RODATA|NOPTR, $64

// func attnExpAVX2(dst, src *float32, n int)
// dst[i] = exp(src[i]) for n elements (n multiple of 8, n >= 8). Same cephes
// degree-5 polynomial and 2^k scaling as the GEMM SiLU epilogue, with the
// input clamped to [-88.37626, 88.37626]. In-place safe (dst == src): each
// element is read into a register before the store of the same iteration.
TEXT ·attnExpAVX2(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ n+16(FP), CX
	SHRQ $3, CX
	TESTQ CX, CX
	JZ    epdone
eploop:
	VMOVUPS (SI), Y0
	VBROADCASTSS attnExpConsts<>+4(SB), Y12
	VMAXPS Y12, Y0, Y0
	VBROADCASTSS attnExpConsts<>+8(SB), Y12
	VMINPS Y12, Y0, Y0
	VBROADCASTSS attnExpConsts<>+12(SB), Y13
	VMULPS Y13, Y0, Y13
	VBROADCASTSS attnExpConsts<>+16(SB), Y14
	VADDPS Y14, Y13, Y13
	VROUNDPS $1, Y13, Y13
	VBROADCASTSS attnExpConsts<>+20(SB), Y14
	VMULPS Y14, Y13, Y14
	VSUBPS Y14, Y0, Y0
	VBROADCASTSS attnExpConsts<>+24(SB), Y14
	VMULPS Y14, Y13, Y14
	VSUBPS Y14, Y0, Y0
	VBROADCASTSS attnExpConsts<>+28(SB), Y15
	VBROADCASTSS attnExpConsts<>+32(SB), Y14
	VFMADD213PS Y14, Y0, Y15
	VBROADCASTSS attnExpConsts<>+36(SB), Y14
	VFMADD213PS Y14, Y0, Y15
	VBROADCASTSS attnExpConsts<>+40(SB), Y14
	VFMADD213PS Y14, Y0, Y15
	VBROADCASTSS attnExpConsts<>+44(SB), Y14
	VFMADD213PS Y14, Y0, Y15
	VBROADCASTSS attnExpConsts<>+48(SB), Y14
	VFMADD213PS Y14, Y0, Y15
	VMULPS Y0, Y0, Y14
	VMULPS Y14, Y15, Y15
	VBROADCASTSS attnExpConsts<>+52(SB), Y14
	VADDPS Y14, Y0, Y14
	VADDPS Y14, Y15, Y15
	VCVTTPS2DQ Y13, Y14
	VPBROADCASTD attnExpConsts<>+60(SB), Y13
	VPADDD Y13, Y14, Y14
	VPSLLD $23, Y14, Y14
	VMULPS Y14, Y15, Y15
	VMOVUPS Y15, (DI)
	ADDQ    $32, DI
	ADDQ    $32, SI
	DECQ    CX
	JNZ     eploop
epdone:
	VZEROUPPER
	RET

// func attnScaleAVX2(dst *float32, s float32, n int)
// dst[i] *= s for n elements (n multiple of 8, n >= 8).
TEXT ·attnScaleAVX2(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), DI
	VBROADCASTSS s+8(FP), Y1
	MOVQ n+16(FP), CX
	SHRQ $3, CX
	TESTQ CX, CX
	JZ    scdone
scloop:
	VMOVUPS (DI), Y0
	VMULPS Y1, Y0, Y0
	VMOVUPS Y0, (DI)
	ADDQ    $32, DI
	DECQ    CX
	JNZ     scloop
scdone:
	VZEROUPPER
	RET

// Deinterleave indices: dword k of the vector selects src index for dst[k].
// We want dst[j] = src[2j] for j in [0,16): indices 0,2,4,...,30 across the
// two 16-float source registers (bit 4 of the index selects the second one).
DATA deint2Idx<>+0(SB)/4, $0
DATA deint2Idx<>+4(SB)/4, $2
DATA deint2Idx<>+8(SB)/4, $4
DATA deint2Idx<>+12(SB)/4, $6
DATA deint2Idx<>+16(SB)/4, $8
DATA deint2Idx<>+20(SB)/4, $10
DATA deint2Idx<>+24(SB)/4, $12
DATA deint2Idx<>+28(SB)/4, $14
DATA deint2Idx<>+32(SB)/4, $16
DATA deint2Idx<>+36(SB)/4, $18
DATA deint2Idx<>+40(SB)/4, $20
DATA deint2Idx<>+44(SB)/4, $22
DATA deint2Idx<>+48(SB)/4, $24
DATA deint2Idx<>+52(SB)/4, $26
DATA deint2Idx<>+56(SB)/4, $28
DATA deint2Idx<>+60(SB)/4, $30
GLOBL deint2Idx<>(SB), RODATA|NOPTR, $64

// func deinterleave2AVX512(dst, src *float32, n int)
// dst[j] = src[2*j] for n elements (n multiple of 16, n >= 16); reads 2n
// floats from src. dst and src must not overlap.
TEXT ·deinterleave2AVX512(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ n+16(FP), CX
	SHRQ $4, CX
	TESTQ CX, CX
	JZ    d2done
	VMOVUPS deint2Idx<>(SB), Z3
d2loop:
	VMOVUPS (SI), Z0      // src[0..15]  (first table)
	VMOVUPS 64(SI), Z2    // src[16..31] (second table)
	VPERMT2PS Z2, Z3, Z0  // Z0 = permute(table=Z0+Z2, idx=Z3)
	VMOVUPS Z0, (DI)
	ADDQ    $128, SI
	ADDQ    $64, DI
	DECQ    CX
	JNZ     d2loop
d2done:
	RET

// func siluZmmAVX512(x *float32, n int)
// x[i] = x[i]*sigmoid(x[i]) for n elements (n multiple of 16, n >= 16).
// 512-bit twin of siluAVX2 (same cephes exp polynomial and Newton-refined
// reciprocal; floor via VRNDSCALEPS — Go's assembler has no Z-form VROUNDPS).
TEXT ·siluZmmAVX512(SB), NOSPLIT, $0-16
	MOVQ x+0(FP), DI
	MOVQ n+8(FP), CX
	SHRQ $4, CX
	TESTQ CX, CX
	JZ    szdone
szloop:
	VMOVUPS (DI), Z0
	VBROADCASTSS attnExpConsts<>+0(SB), Z8
	VXORPS Z8, Z0, Z8
	VBROADCASTSS attnExpConsts<>+4(SB), Z9
	VMAXPS Z9, Z8, Z8
	VBROADCASTSS attnExpConsts<>+8(SB), Z9
	VMINPS Z9, Z8, Z8
	VBROADCASTSS attnExpConsts<>+12(SB), Z9
	VMULPS Z9, Z8, Z9
	VBROADCASTSS attnExpConsts<>+16(SB), Z10
	VADDPS Z10, Z9, Z9
	VRNDSCALEPS $0x01, Z9, Z9
	VBROADCASTSS attnExpConsts<>+20(SB), Z10
	VMULPS Z10, Z9, Z10
	VSUBPS Z10, Z8, Z8
	VBROADCASTSS attnExpConsts<>+24(SB), Z10
	VMULPS Z10, Z9, Z10
	VSUBPS Z10, Z8, Z8
	VBROADCASTSS attnExpConsts<>+28(SB), Z11
	VBROADCASTSS attnExpConsts<>+32(SB), Z10
	VFMADD213PS Z10, Z8, Z11
	VBROADCASTSS attnExpConsts<>+36(SB), Z10
	VFMADD213PS Z10, Z8, Z11
	VBROADCASTSS attnExpConsts<>+40(SB), Z10
	VFMADD213PS Z10, Z8, Z11
	VBROADCASTSS attnExpConsts<>+44(SB), Z10
	VFMADD213PS Z10, Z8, Z11
	VBROADCASTSS attnExpConsts<>+48(SB), Z10
	VFMADD213PS Z10, Z8, Z11
	VMULPS Z8, Z8, Z10
	VMULPS Z10, Z11, Z11
	VBROADCASTSS attnExpConsts<>+52(SB), Z10
	VADDPS Z10, Z8, Z10
	VADDPS Z10, Z11, Z11
	VCVTTPS2DQ Z9, Z10
	VPBROADCASTD attnExpConsts<>+60(SB), Z9
	VPADDD Z9, Z10, Z10
	VPSLLD $23, Z10, Z10
	VMULPS Z10, Z11, Z11
	VBROADCASTSS attnExpConsts<>+52(SB), Z10
	VADDPS Z11, Z10, Z9
	VRCP14PS Z9, Z10
	VMULPS Z10, Z9, Z8
	VBROADCASTSS attnExpConsts<>+56(SB), Z11
	VSUBPS Z8, Z11, Z8
	VMULPS Z8, Z10, Z10
	VMULPS Z10, Z0, Z0
	VMOVUPS Z0, (DI)
	ADDQ    $64, DI
	DECQ    CX
	JNZ     szloop
szdone:
	RET

// func max5RowAVX512(out, x *float32, n int)
// out[j] = max(x[j], x[j+1], ..., x[j+4]) for n elements (n multiple of 16,
// n >= 16); reads n+4 floats from x (caller guarantees x has n+4 readable).
TEXT ·max5RowAVX512(SB), NOSPLIT, $0-24
	MOVQ out+0(FP), DI
	MOVQ x+8(FP), SI
	MOVQ n+16(FP), CX
	SHRQ $4, CX
	TESTQ CX, CX
	JZ    m5done
m5loop:
	VMOVUPS (SI), Z0
	VMAXPS 4(SI), Z0, Z0
	VMAXPS 8(SI), Z0, Z0
	VMAXPS 12(SI), Z0, Z0
	VMAXPS 16(SI), Z0, Z0
	VMOVUPS Z0, (DI)
	ADDQ    $64, SI
	ADDQ    $64, DI
	DECQ    CX
	JNZ     m5loop
m5done:
	RET

// func maxIntoAVX512(out, x *float32, n int)
// out[j] = max(out[j], x[j]) for n elements (n multiple of 16, n >= 16).
TEXT ·maxIntoAVX512(SB), NOSPLIT, $0-24
	MOVQ out+0(FP), DI
	MOVQ x+8(FP), SI
	MOVQ n+16(FP), CX
	SHRQ $4, CX
	TESTQ CX, CX
	JZ    midone
miloop:
	VMOVUPS (DI), Z0
	VMAXPS  (SI), Z0, Z0
	VMOVUPS Z0, (DI)
	ADDQ    $64, SI
	ADDQ    $64, DI
	DECQ    CX
	JNZ     miloop
midone:
	RET
