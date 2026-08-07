//go:build amd64

#include "textflag.h"

// func fmaddScalarAVX2(out, x *float32, w float32, n int)
// out[i] += w*x[i] for n elements (n multiple of 8, n>=8).
// Frame: out+0, x+8, w+16, n+24 (w is 4 bytes, n 8-aligned at 24).
TEXT ·fmaddScalarAVX2(SB), NOSPLIT, $0-32
	MOVQ out+0(FP), DI
	MOVQ x+8(FP), SI
	VBROADCASTSS w+16(FP), Y2
	MOVQ n+24(FP), CX

	MOVQ CX, R8
	SHRQ $5, R8               // n/32
	TESTQ R8, R8
	JZ   fs8chk

fs32: // 4x unrolled, 32 floats per iteration
	VMOVUPS (DI), Y0
	VMOVUPS (SI), Y1
	VFMADD231PS Y1, Y2, Y0
	VMOVUPS Y0, (DI)
	VMOVUPS 32(DI), Y0
	VMOVUPS 32(SI), Y1
	VFMADD231PS Y1, Y2, Y0
	VMOVUPS Y0, 32(DI)
	VMOVUPS 64(DI), Y0
	VMOVUPS 64(SI), Y1
	VFMADD231PS Y1, Y2, Y0
	VMOVUPS Y0, 64(DI)
	VMOVUPS 96(DI), Y0
	VMOVUPS 96(SI), Y1
	VFMADD231PS Y1, Y2, Y0
	VMOVUPS Y0, 96(DI)
	ADDQ $128, DI
	ADDQ $128, SI
	DECQ R8
	JNZ  fs32

fs8chk:
	MOVQ n+24(FP), CX
	ANDQ $31, CX
	SHRQ $3, CX               // remaining 8-wide blocks
	TESTQ CX, CX
	JZ   fsdone
fs8:
	VMOVUPS (DI), Y0
	VMOVUPS (SI), Y1
	VFMADD231PS Y1, Y2, Y0
	VMOVUPS Y0, (DI)
	ADDQ $32, DI
	ADDQ $32, SI
	DECQ CX
	JNZ  fs8

fsdone:
	VZEROUPPER
	RET

// Constants for geluErfAVX2 (float32 bit patterns). The exp block replicates
// vek32's Exp_Len8x_AVX2_F32 constants exactly so the fused kernel stays
// bit-identical to the previous vek32-op pipeline.
DATA geluConsts<>+0(SB)/4, $0x3f3504f3   // invSqrt2
DATA geluConsts<>+4(SB)/4, $0x7fffffff   // abs mask
DATA geluConsts<>+8(SB)/4, $0x3ea7ba05   // erfP = 0.3275911
DATA geluConsts<>+12(SB)/4, $0x3f800000  // 1.0 (also exp exponent bias and Inv Newton constant)
DATA geluConsts<>+16(SB)/4, $0x80000000  // sign mask
DATA geluConsts<>+20(SB)/4, $0xc2a00000  // -80.0
DATA geluConsts<>+24(SB)/4, $0x3f87dc22  // erf a4 = 1.061405429
DATA geluConsts<>+28(SB)/4, $0xbfba00e3  // erf a3 = -1.453152027
DATA geluConsts<>+32(SB)/4, $0x3fb5f0e3  // erf a2 = 1.421413741
DATA geluConsts<>+36(SB)/4, $0xbe91a98e  // erf a1 = -0.284496736
DATA geluConsts<>+40(SB)/4, $0x3e827906  // erf a0 = 0.254829592
DATA geluConsts<>+44(SB)/4, $0x42b17218  // exp max x
DATA geluConsts<>+48(SB)/4, $0xc2ce8ed0  // exp min x
DATA geluConsts<>+52(SB)/4, $0x3f000000  // 0.5
DATA geluConsts<>+56(SB)/4, $0x3fb8aa3b  // log2(e)
DATA geluConsts<>+60(SB)/4, $0xbf318000  // -ln2 hi
DATA geluConsts<>+64(SB)/4, $0x395e8083  // ln2 lo
DATA geluConsts<>+68(SB)/4, $0x3ab743ce  // exp poly c1 (2nd Horner term)
DATA geluConsts<>+72(SB)/4, $0x39506967  // exp poly c0 (leading Horner term)
DATA geluConsts<>+76(SB)/4, $0x3c088908  // exp poly p2
DATA geluConsts<>+80(SB)/4, $0x3d2aa9c1  // exp poly p3
DATA geluConsts<>+84(SB)/4, $0x3e2aaaaa  // exp poly p4
DATA geluConsts<>+88(SB)/4, $0x7f7fffff  // max finite float32
DATA geluConsts<>+92(SB)/4, $0x7f800000  // +Inf
DATA geluConsts<>+96(SB)/4, $0xff800000  // -Inf
DATA geluConsts<>+100(SB)/4, $0xbf800000 // -1.0
GLOBL geluConsts<>(SB), RODATA|NOPTR, $104

// func geluErfAVX2(dst, src *float32, n int)
// dst[i] = 0.5*src[i]*(1+erf(src[i]/sqrt(2))) for n elements (n multiple of
// 8; n == 0 is a no-op). In-place safe (dst == src): each block is read
// before stored.
//
// Bit-exact with the scalar pipeline of vek32 ops: same A&S 7.1.26 erf
// polynomial, same rcp+Newton reciprocal (vek32 Inv), same vectorized exp
// (vek32 Exp_Len8x_AVX2_F32 instruction-for-instruction), same NaN/Inf
// fixups.
//
// Register use: Y13=x (original), Y12=z=x/sqrt2, Y11=|z|, Y10=t, Y9=poly,
// Y8=exp work, Y0-Y7/Y14/Y15 temps and broadcasted constants.
TEXT ·geluErfAVX2(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ n+16(FP), CX
	XORL AX, AX
	TESTQ CX, CX
	JZ    geludone                  // n == 0: no-op (loop below is do-while)

gelu8:
	VMOVUPS (SI)(AX*4), Y13          // x
	// z = x * invSqrt2
	VBROADCASTSS geluConsts<>+0(SB), Y0
	VMULPS Y0, Y13, Y12
	// ax = |z|
	VBROADCASTSS geluConsts<>+4(SB), Y0
	VANDPS Y0, Y12, Y11
	// t = 1 / (1 + erfP*ax): mul, add, then rcp+Newton (vek32 Inv)
	VBROADCASTSS geluConsts<>+8(SB), Y0
	VMULPS Y11, Y0, Y10
	VBROADCASTSS geluConsts<>+12(SB), Y0
	VADDPS Y0, Y10, Y10
	VRCPPS Y10, Y1
	VMOVAPS Y10, Y2
	VBROADCASTSS geluConsts<>+12(SB), Y3
	VFMSUB213PS Y3, Y1, Y2           // Y2 = t*r - 1.0
	VFNMADD132PS Y1, Y1, Y2          // Y2 = r - Y2*r = r*(2 - t*r)
	VMOVAPS Y2, Y10
	// poly = ((((a4*t+a3)*t+a2)*t+a1)*t+a0)*t
	VBROADCASTSS geluConsts<>+24(SB), Y9
	VMULPS Y10, Y9, Y9
	VBROADCASTSS geluConsts<>+28(SB), Y0
	VADDPS Y0, Y9, Y9
	VMULPS Y10, Y9, Y9
	VBROADCASTSS geluConsts<>+32(SB), Y0
	VADDPS Y0, Y9, Y9
	VMULPS Y10, Y9, Y9
	VBROADCASTSS geluConsts<>+36(SB), Y0
	VADDPS Y0, Y9, Y9
	VMULPS Y10, Y9, Y9
	VBROADCASTSS geluConsts<>+40(SB), Y0
	VADDPS Y0, Y9, Y9
	VMULPS Y10, Y9, Y9
	// e = exp(max(-(ax*ax), -80))
	VMULPS Y11, Y11, Y8
	VBROADCASTSS geluConsts<>+16(SB), Y0
	VXORPS Y0, Y8, Y8
	VBROADCASTSS geluConsts<>+20(SB), Y0
	VMAXPS Y0, Y8, Y8
	// exp(Y8): vek32 Exp_Len8x_AVX2_F32 sequence
	VBROADCASTSS geluConsts<>+56(SB), Y14
	VBROADCASTSS geluConsts<>+52(SB), Y1
	VFMADD213PS Y1, Y8, Y14          // Y14 = log2e*e + 0.5
	VROUNDPS $0x01, Y14, Y14         // n = floor(...)
	VBROADCASTSS geluConsts<>+60(SB), Y15
	VFMADD213PS Y8, Y14, Y15         // Y15 = ln2hi*n + e
	VBROADCASTSS geluConsts<>+64(SB), Y1
	VFMADD231PS Y1, Y14, Y15         // Y15 += ln2lo*n
	VMULPS Y15, Y15, Y0              // g*g
	VBROADCASTSS geluConsts<>+72(SB), Y2
	VBROADCASTSS geluConsts<>+68(SB), Y1
	VFMADD213PS Y1, Y15, Y2          // c0*g + c1 (vek Horner order)
	VBROADCASTSS geluConsts<>+76(SB), Y1
	VFMADD213PS Y1, Y15, Y2
	VBROADCASTSS geluConsts<>+80(SB), Y1
	VFMADD213PS Y1, Y15, Y2
	VBROADCASTSS geluConsts<>+84(SB), Y1
	VFMADD213PS Y1, Y15, Y2
	VBROADCASTSS geluConsts<>+52(SB), Y1
	VFMADD213PS Y1, Y15, Y2          // ...*g + 0.5
	VFMADD213PS Y15, Y0, Y2          // ...*g^2 + g
	VCVTTPS2DQ Y14, Y3
	VPSLLD $0x17, Y3, Y3
	VPBROADCASTD geluConsts<>+12(SB), Y4
	VPADDD Y4, Y3, Y3                // 2^n bits
	VFMADD213PS Y3, Y3, Y2           // Y2 = Y2*2^n + 2^n
	VBROADCASTSS geluConsts<>+44(SB), Y5
	VCMPPS $0x01, Y8, Y5, Y5         // overflow: expMax < e
	VBROADCASTSS geluConsts<>+88(SB), Y6
	VBLENDVPS Y5, Y6, Y2, Y5
	VBROADCASTSS geluConsts<>+48(SB), Y6
	VCMPPS $0x02, Y8, Y6, Y6         // underflow: e <= expMin
	VANDPS Y5, Y6, Y8                // e = exp result
	// v = 1 - poly*e
	VMULPS Y8, Y9, Y9
	VBROADCASTSS geluConsts<>+16(SB), Y0
	VXORPS Y0, Y9, Y9
	VBROADCASTSS geluConsts<>+12(SB), Y1
	VADDPS Y1, Y9, Y9
	// sign restore: v = (z < 0) ? -v : v
	VXORPS Y0, Y9, Y7                // -v (Y0 = sign mask)
	VXORPS Y6, Y6, Y6
	VCMPPS $0x11, Y6, Y12, Y5        // z < 0 (LT_OQ)
	VBLENDVPS Y5, Y7, Y9, Y9
	// NaN fixup: v NaN (non-finite input) -> erf(+-Inf) = +-1, NaN stays
	VCMPPS $0x04, Y9, Y9, Y5         // v != v (NEQ_UQ)
	VBROADCASTSS geluConsts<>+92(SB), Y6
	VCMPPS $0x00, Y6, Y12, Y7        // z == +Inf
	VBROADCASTSS geluConsts<>+12(SB), Y1
	VBLENDVPS Y7, Y1, Y9, Y2         // +Inf -> 1
	VBROADCASTSS geluConsts<>+96(SB), Y6
	VCMPPS $0x00, Y6, Y12, Y7        // z == -Inf
	VBROADCASTSS geluConsts<>+100(SB), Y3
	VBLENDVPS Y7, Y3, Y2, Y2         // -Inf -> -1
	VBLENDVPS Y5, Y2, Y9, Y9         // apply on NaN lanes only
	// gelu tail: v = (v + 1) * x * 0.5
	VBROADCASTSS geluConsts<>+12(SB), Y1
	VADDPS Y1, Y9, Y9
	VMULPS Y13, Y9, Y9
	VBROADCASTSS geluConsts<>+52(SB), Y1
	VMULPS Y1, Y9, Y9
	VMOVUPS Y9, (DI)(AX*4)
	ADDQ $0x08, AX
	CMPQ AX, CX
	JB   gelu8

geludone:
	VZEROUPPER
	RET

// func transpose8x8F32(dst *float32, ldDst int, src *float32, ldSrc int)
// Transposes an 8x8 float32 block: dst[j*ldDst+i] = src[i*ldSrc+j].
// Pure data movement (no arithmetic): bit-exact by construction.
TEXT ·transpose8x8F32(SB), NOSPLIT, $0-32
	MOVQ dst+0(FP), DI
	MOVQ ldDst+8(FP), R8
	SHLQ $2, R8
	MOVQ src+16(FP), SI
	MOVQ ldSrc+24(FP), CX
	SHLQ $2, CX

	VMOVUPS (SI), Y0
	ADDQ    CX, SI
	VMOVUPS (SI), Y1
	ADDQ    CX, SI
	VMOVUPS (SI), Y2
	ADDQ    CX, SI
	VMOVUPS (SI), Y3
	ADDQ    CX, SI
	VMOVUPS (SI), Y4
	ADDQ    CX, SI
	VMOVUPS (SI), Y5
	ADDQ    CX, SI
	VMOVUPS (SI), Y6
	ADDQ    CX, SI
	VMOVUPS (SI), Y7

	VUNPCKLPS Y1, Y0, Y8
	VUNPCKHPS Y1, Y0, Y9
	VUNPCKLPS Y3, Y2, Y10
	VUNPCKHPS Y3, Y2, Y11
	VUNPCKLPS Y5, Y4, Y12
	VUNPCKHPS Y5, Y4, Y13
	VUNPCKLPS Y7, Y6, Y14
	VUNPCKHPS Y7, Y6, Y15

	VSHUFPS $0x44, Y10, Y8, Y0
	VSHUFPS $0xEE, Y10, Y8, Y1
	VSHUFPS $0x44, Y11, Y9, Y2
	VSHUFPS $0xEE, Y11, Y9, Y3
	VSHUFPS $0x44, Y14, Y12, Y4
	VSHUFPS $0xEE, Y14, Y12, Y5
	VSHUFPS $0x44, Y15, Y13, Y6
	VSHUFPS $0xEE, Y15, Y13, Y7

	VPERM2F128 $0x20, Y4, Y0, Y8
	VPERM2F128 $0x20, Y5, Y1, Y9
	VPERM2F128 $0x20, Y6, Y2, Y10
	VPERM2F128 $0x20, Y7, Y3, Y11
	VPERM2F128 $0x31, Y4, Y0, Y12
	VPERM2F128 $0x31, Y5, Y1, Y13
	VPERM2F128 $0x31, Y6, Y2, Y14
	VPERM2F128 $0x31, Y7, Y3, Y15

	VMOVUPS Y8, (DI)
	ADDQ    R8, DI
	VMOVUPS Y9, (DI)
	ADDQ    R8, DI
	VMOVUPS Y10, (DI)
	ADDQ    R8, DI
	VMOVUPS Y11, (DI)
	ADDQ    R8, DI
	VMOVUPS Y12, (DI)
	ADDQ    R8, DI
	VMOVUPS Y13, (DI)
	ADDQ    R8, DI
	VMOVUPS Y14, (DI)
	ADDQ    R8, DI
	VMOVUPS Y15, (DI)

	VZEROUPPER
	RET

// func fmadd3AVX2(out, x *float32, w0, w1, w2 float32, n int)
// out[i] += w0*x[i] + w1*x[i+1] + w2*x[i+2] for n elements (n multiple of 8;
// n == 0 is a no-op). x must have at least n+2 valid elements from the given
// pointer.
// Same FMA order as three sequential fmaddScalarInto passes: bit-exact.
TEXT ·fmadd3AVX2(SB), NOSPLIT, $0-40
	MOVQ out+0(FP), DI
	MOVQ x+8(FP), SI
	VBROADCASTSS w0+16(FP), Y3
	VBROADCASTSS w1+20(FP), Y4
	VBROADCASTSS w2+24(FP), Y5
	MOVQ n+32(FP), CX
	XORL AX, AX
	TESTQ CX, CX
	JZ    fm3done                   // n == 0: no-op (loop below is do-while)

fm3:
	VMOVUPS (DI)(AX*4), Y0
	VMOVUPS (SI)(AX*4), Y1
	VFMADD231PS Y1, Y3, Y0
	VMOVUPS 4(SI)(AX*4), Y1
	VFMADD231PS Y1, Y4, Y0
	VMOVUPS 8(SI)(AX*4), Y1
	VFMADD231PS Y1, Y5, Y0
	VMOVUPS Y0, (DI)(AX*4)
	ADDQ $0x08, AX
	CMPQ AX, CX
	JB   fm3

fm3done:
	VZEROUPPER
	RET
