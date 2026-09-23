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

// func geluErfAVX512(dst, src *float32, n int)
// 16-wide AVX-512 port of geluErfAVX2: same per-lane op sequence, same
// constants, same NaN/Inf fixups — bit-exact with the vek32 pipeline by
// construction (cross-validated in simd_crossval_test.go). n multiple of 16,
// n == 0 no-op. In-place safe.
//
// Register use: Z0=x, Z1=z, Z2=ax, Z3=t, Z4=poly/v, Z5=e/exp, Z6-Z15 temps;
// K1-K6 comparison masks for the fixup blends.
TEXT ·geluErfAVX512(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ n+16(FP), CX
	XORL AX, AX
	TESTQ CX, CX
	JZ    geluzDone

geluz:
	VMOVUPS (SI)(AX*4), Z0                     // x
	VBROADCASTSS geluConsts<>+0(SB), Z6        // invSqrt2
	VMULPS Z6, Z0, Z1                          // z = x*invSqrt2
	VBROADCASTSS geluConsts<>+4(SB), Z6        // abs mask
	VANDPS Z6, Z1, Z2                          // ax = |z|
	VBROADCASTSS geluConsts<>+8(SB), Z6        // erfP
	VMULPS Z2, Z6, Z3                          // erfP*ax
	VBROADCASTSS geluConsts<>+12(SB), Z6       // 1.0
	VADDPS Z6, Z3, Z3                          // 1 + erfP*ax
	// t = 1/(1+erfP*ax) via VRCPPS+Newton on two 256-bit halves: VRCPPS has
	// no 512-bit form and VRCP14PS differs from vek32's rcp, so each half
	// runs the exact vek32 Inv sequence and VINSERTF64X4 recombines.
	VEXTRACTF32X8 $1, Z3, Y8                   // Y8 = t.hi
	VRCPPS Y3, Y9                              // Y9 = rcp(t.lo)  (Y3 = low half of Z3)
	VRCPPS Y8, Y10                             // Y10 = rcp(t.hi)
	VMOVAPS Y3, Y11
	VBROADCASTSS geluConsts<>+12(SB), Y12      // 1.0
	VFMSUB213PS Y12, Y9, Y11                   // Y11 = t.lo*r - 1
	VFNMADD132PS Y9, Y9, Y11                   // Y11 = r - Y11*r
	VMOVAPS Y8, Y13
	VFMSUB213PS Y12, Y10, Y13                  // Y13 = t.hi*r - 1
	VFNMADD132PS Y10, Y10, Y13                 // Y13 = r - Y13*r
	VINSERTF64X4 $1, Y13, Z11, Z3              // Z3 = t (16 lanes)
	// erf poly: ((((a4*t+a3)*t+a2)*t+a1)*t+a0)*t
	VBROADCASTSS geluConsts<>+24(SB), Z4
	VMULPS Z3, Z4, Z4
	VBROADCASTSS geluConsts<>+28(SB), Z6
	VADDPS Z6, Z4, Z4
	VMULPS Z3, Z4, Z4
	VBROADCASTSS geluConsts<>+32(SB), Z6
	VADDPS Z6, Z4, Z4
	VMULPS Z3, Z4, Z4
	VBROADCASTSS geluConsts<>+36(SB), Z6
	VADDPS Z6, Z4, Z4
	VMULPS Z3, Z4, Z4
	VBROADCASTSS geluConsts<>+40(SB), Z6
	VADDPS Z6, Z4, Z4
	VMULPS Z3, Z4, Z4
	// e = exp(max(-(ax*ax), -80))
	VMULPS Z2, Z2, Z5                          // ax*ax
	VBROADCASTSS geluConsts<>+16(SB), Z6       // sign mask
	VXORPS Z6, Z5, Z5                          // -(ax*ax)
	VBROADCASTSS geluConsts<>+20(SB), Z6       // -80
	VMAXPS Z6, Z5, Z5                          // max(-80, e)
	// exp core (vek32 Exp sequence; identical to ctcRowExpAVX512)
	VBROADCASTSS geluConsts<>+56(SB), Z10      // log2e
	VBROADCASTSS geluConsts<>+52(SB), Z1       // 0.5
	VFMADD213PS Z1, Z5, Z10                    // Z10 = e*log2e + 0.5
	VRNDSCALEPS $0x01, Z10, Z10                // n = floor
	VBROADCASTSS geluConsts<>+60(SB), Z11      // ln2hi
	VFMADD213PS Z5, Z10, Z11                   // Z11 = n*ln2hi + e
	VBROADCASTSS geluConsts<>+64(SB), Z1       // ln2lo
	VFMADD231PS Z1, Z10, Z11                   // += n*ln2lo
	VMULPS Z11, Z11, Z12                       // g²
	VBROADCASTSS geluConsts<>+72(SB), Z13      // exp poly c0
	VBROADCASTSS geluConsts<>+68(SB), Z1
	VFMADD213PS Z1, Z11, Z13
	VBROADCASTSS geluConsts<>+76(SB), Z1
	VFMADD213PS Z1, Z11, Z13
	VBROADCASTSS geluConsts<>+80(SB), Z1
	VFMADD213PS Z1, Z11, Z13
	VBROADCASTSS geluConsts<>+84(SB), Z1
	VFMADD213PS Z1, Z11, Z13
	VBROADCASTSS geluConsts<>+52(SB), Z1
	VFMADD213PS Z1, Z11, Z13
	VFMADD213PS Z11, Z12, Z13                  // g²*poly + g
	VCVTTPS2DQ Z10, Z14
	VPSLLD $0x17, Z14, Z14
	VPBROADCASTD geluConsts<>+12(SB), Z15
	VPADDD Z15, Z14, Z14                       // 2^n bits
	VFMADD213PS Z14, Z14, Z13                  // poly*2^n + 2^n
	// fixups: overflow -> max finite; underflow -> 0
	VBROADCASTSS geluConsts<>+44(SB), Z6       // expMax
	VCMPPS $0x01, Z5, Z6, K1                   // K1 = expMax < e
	VBROADCASTSS geluConsts<>+88(SB), Z7       // max finite
	VBLENDMPS Z7, Z13, K1, Z8                  // Z8 = K1 ? maxfinite : expcore
	VBROADCASTSS geluConsts<>+48(SB), Z6       // expMin
	VCMPPS $0x02, Z5, Z6, K2                   // K2 = expMin <= e (keep mask)
	VXORPS Z9, Z9, Z9
	VBLENDMPS Z8, Z9, K2, Z8                   // underflow lanes -> 0
	// v = 1 - poly*e
	VMULPS Z8, Z4, Z4
	VBROADCASTSS geluConsts<>+16(SB), Z6       // sign mask
	VXORPS Z6, Z4, Z4                          // -(poly*e)
	VBROADCASTSS geluConsts<>+12(SB), Z6
	VADDPS Z6, Z4, Z4                          // 1 - poly*e
	// sign restore: v = (z < 0) ? -v : v
	// NOTE: Z6 currently holds 1.0 (broadcast for the v = 1 - poly*e add
	// above), NOT the sign mask — reload it before the negation. (Using the
	// stale 1.0 here computes 1.0^v, which is ~0 whenever v ~ 1 and silently
	// zeroed every negative lane.)
	VBROADCASTSS geluConsts<>+16(SB), Z6       // sign mask
	VXORPS Z6, Z4, Z7                          // -v
	VXORPS Z9, Z9, Z9
	VCMPPS $0x11, Z9, Z0, K3                   // K3 = z < 0 (Z0=x: sign(z)==sign(x), Z1 is exp-scratch)
	VBLENDMPS Z7, Z4, K3, Z4
	// NaN fixup: erf(+Inf)=1, erf(-Inf)=-1
	VCMPPS $0x04, Z4, Z4, K4                   // v != v (NaN)
	VBROADCASTSS geluConsts<>+92(SB), Z6       // +Inf
	VCMPPS $0x00, Z6, Z0, K5                   // z == +Inf
	VBROADCASTSS geluConsts<>+12(SB), Z6       // 1.0
	VBLENDMPS Z6, Z4, K5, Z8                   // Z8 = +Inf ? 1 : v
	VBROADCASTSS geluConsts<>+96(SB), Z6       // -Inf
	VCMPPS $0x00, Z6, Z0, K6                   // z == -Inf
	VBROADCASTSS geluConsts<>+100(SB), Z6      // -1.0
	VBLENDMPS Z6, Z8, K6, Z8                   // Z8 = -Inf ? -1 : Z8
	VBLENDMPS Z8, Z4, K4, Z4                   // NaN lanes only
	// gelu tail: v = (v+1)*x*0.5
	VBROADCASTSS geluConsts<>+12(SB), Z6
	VADDPS Z6, Z4, Z4
	VMULPS Z0, Z4, Z4
	VBROADCASTSS geluConsts<>+52(SB), Z6       // 0.5
	VMULPS Z6, Z4, Z4
	VMOVUPS Z4, (DI)(AX*4)
	ADDQ $0x10, AX
	CMPQ AX, CX
	JB    geluz

geluzDone:
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

// func ctcRowExpAVX512(row *float32, n int, m float32)
// row[i] = exp(max(row[i]-m, -80)) for n elements (n multiple of 16; n == 0
// is a no-op). In-place safe (row is read then stored per block).
//
// Fuses the vek32 SubNumber/MaximumNumber/Exp_Inplace triple pass of the CTC
// epilogue into one ZMM pass. The exp core replicates vek32's
// Exp_Len8x_AVX2_F32 instruction-for-instruction (same as geluErfAVX2's exp
// block). The overflow/underflow fixup branches are omitted: every exp
// argument here lies in [-80, 0] after the clamp, where the polynomial result
// already equals the vek32 pipeline bit-for-bit (exp(-80) ~ 1.8e-35 is a
// normal float and below no fixup threshold).
TEXT ·ctcRowExpAVX512(SB), NOSPLIT, $0-20
	MOVQ row+0(FP), DI
	MOVQ n+8(FP), CX
	VMOVSS m+16(FP), X16
	VBROADCASTSS X16, Z16                       // m (loop-invariant: Z15/Z17 would
	                                            //  collide with ln2hi / -80 scratch)
	VBROADCASTSS geluConsts<>+20(SB), Z17       // -80
	XORL AX, AX
	TESTQ CX, CX
	JZ    ctcExDone

ctcEx:
	VMOVUPS (DI)(AX*4), Z0                      // row
	// Go asm binary ops are AT&T order: Go(a, b, c) => c = b - a, so the
	// minuend (row) is the SECOND operand. e = row - m.
	VSUBPS Z16, Z0, Z0                          // e = row - m
	VMAXPS Z17, Z0, Z0                          // e = max(-80, e)
	VBROADCASTSS geluConsts<>+56(SB), Z14       // log2e (consumed by FMA)
	VBROADCASTSS geluConsts<>+52(SB), Z1        // 0.5
	VFMADD213PS Z1, Z0, Z14                     // Z14 = e*log2e + 0.5
	VRNDSCALEPS $0x01, Z14, Z14                 // n = floor(Z14)
	VBROADCASTSS geluConsts<>+60(SB), Z15      // ln2hi
	VFMADD213PS Z0, Z14, Z15                   // Z15 = n*ln2hi + e
	VBROADCASTSS geluConsts<>+64(SB), Z1        // ln2lo
	VFMADD231PS Z1, Z14, Z15                   // += n*ln2lo
	VMULPS Z15, Z15, Z0                       // g = g*g
	VBROADCASTSS geluConsts<>+72(SB), Z2        // exp poly c0 (leading)
	VBROADCASTSS geluConsts<>+68(SB), Z1        // c1
	VFMADD213PS Z1, Z15, Z2                    // g*c0 + c1
	VBROADCASTSS geluConsts<>+76(SB), Z1
	VFMADD213PS Z1, Z15, Z2
	VBROADCASTSS geluConsts<>+80(SB), Z1
	VFMADD213PS Z1, Z15, Z2
	VBROADCASTSS geluConsts<>+84(SB), Z1
	VFMADD213PS Z1, Z15, Z2
	VBROADCASTSS geluConsts<>+52(SB), Z1        // 0.5
	VFMADD213PS Z1, Z15, Z2                    // *g + 0.5
	VFMADD213PS Z15, Z0, Z2                    // g²*poly + g
	VCVTTPS2DQ Z14, Z3
	VPSLLD $0x17, Z3, Z3
	VPBROADCASTD geluConsts<>+12(SB), Z4
	VPADDD Z4, Z3, Z3                           // 2^n bits
	VFMADD213PS Z3, Z3, Z2                      // poly*2^n + 2^n
	VMOVUPS Z2, (DI)(AX*4)
	ADDQ $0x10, AX
	CMPQ AX, CX
	JB    ctcEx

ctcExDone:
	VZEROUPPER
	RET

// func im2row3x3AVX512(dst, x *float32, rowOff *int, groups, K, HW, Cg, srcOff int)
//
// Vectorized interior of im2rowFast for the 3x3 sW=1 detector path.
// For each channel c and kernel row kh it copies 3 consecutive floats
// x[base+g..g+2] (base = c*HW + rowOff[kh] + srcOff) into B rows of 4
// consecutive output pixels at column 9c+3kh. Pixels are processed 4 at a
// time via one 3x4 -> 4x3 register transpose.
//
// Spill discipline: the 16-byte stores write a 4th lane at column+3, which
// is always owned by a LATER write within the same call (kh1 fixes kh0's
// spill, kh2 fixes kh1's, channel c+1's kh0 fixes channel c's kh2). The one
// exception is (c=Cg-1, kh=2), whose spill would land on the next row's
// column 0 with no later writer — that block uses exact 8+4-byte stores.
//
// Requirements (checked by the caller):
//   - dst = &B[(pi+s0-ow)*K], x = &x[xG], rowOff[0..2] = image-row offsets
//     (-1 for padded), srcOff = s0 - pL
//   - groups*4 + 2 <= number of interior pixels (bounds the +2 over-read)
//   - every vector row's spill target row is written by the caller's scalar
//     tail or a later vector row within this call
TEXT ·im2row3x3AVX512(SB), NOSPLIT, $0-64
	MOVQ dst+0(FP), R13                        // channel dst base (col 0)
	MOVQ x+8(FP), SI                           // x base (c=0 plane)
	MOVQ rowOff+16(FP), R10
	MOVQ groups+24(FP), R11
	MOVQ K+32(FP), AX
	SHLQ $2, AX
	MOVQ AX, R8                                // dst row stride bytes
	MOVQ AX, BX
	SHLQ $2, BX                                // 4-row group stride bytes
	MOVQ HW+40(FP), AX
	SHLQ $2, AX
	MOVQ AX, R9                                // channel plane stride bytes
	MOVQ Cg+48(FP), R12
	MOVQ srcOff+56(FP), AX
	SHLQ $2, AX
	MOVQ AX, R15                               // srcOff bytes
	VXORPS X11, X11, X11

channel:
	// kh = 0, column 9c+0
	MOVQ (R10), DX
	TESTQ DX, DX
	JS    kh0z
	LEAQ (SI)(DX*4), DX
	ADDQ R15, DX
	MOVQ  R13, DI
	MOVQ  R11, CX
kh0g:
	VMOVUPS (DX), X0
	VMOVUPS 4(DX), X1
	VMOVUPS 8(DX), X2
	VUNPCKLPS X1, X0, X3                       // [a0,b0,a1,b1]
	VUNPCKHPS X1, X0, X4                       // [a2,b2,a3,b3]
	VUNPCKLPS X2, X2, X5                       // [c0,c0,c1,c1]
	VUNPCKHPS X2, X2, X6                       // [c2,c2,c3,c3]
	VSHUFPS $0x44, X5, X3, X7                  // [a0,b0,c0,c0]
	VSHUFPS $0xEE, X5, X3, X8                  // [a1,b1,c1,c1]
	VSHUFPS $0x44, X6, X4, X9                  // [a2,b2,c2,c2]
	VSHUFPS $0xEE, X6, X4, X10                 // [a3,b3,c3,c3]
	VMOVUPS X7, (DI)
	VMOVUPS X8, (DI)(R8*1)
	VMOVUPS X9, (DI)(R8*2)
	LEAQ (DI)(R8*2), AX
	VMOVUPS X10, (AX)(R8*1)
	ADDQ BX, DI
	ADDQ $16, DX
	DECQ CX
	JNZ   kh0g
	JMP   kh1
kh0z:
	MOVQ R13, DI
	MOVQ R11, CX
kh0zg:
	VMOVUPS X11, (DI)
	VMOVUPS X11, (DI)(R8*1)
	VMOVUPS X11, (DI)(R8*2)
	LEAQ (DI)(R8*2), AX
	VMOVUPS X11, (AX)(R8*1)
	ADDQ BX, DI
	DECQ CX
	JNZ   kh0zg

kh1:
	// kh = 1, column 9c+3
	MOVQ 8(R10), DX
	TESTQ DX, DX
	JS    kh1z
	LEAQ (SI)(DX*4), DX
	ADDQ R15, DX
	LEAQ 12(R13), DI
	MOVQ  R11, CX
kh1g:
	VMOVUPS (DX), X0
	VMOVUPS 4(DX), X1
	VMOVUPS 8(DX), X2
	VUNPCKLPS X1, X0, X3
	VUNPCKHPS X1, X0, X4
	VUNPCKLPS X2, X2, X5
	VUNPCKHPS X2, X2, X6
	VSHUFPS $0x44, X5, X3, X7
	VSHUFPS $0xEE, X5, X3, X8
	VSHUFPS $0x44, X6, X4, X9
	VSHUFPS $0xEE, X6, X4, X10
	VMOVUPS X7, (DI)
	VMOVUPS X8, (DI)(R8*1)
	VMOVUPS X9, (DI)(R8*2)
	LEAQ (DI)(R8*2), AX
	VMOVUPS X10, (AX)(R8*1)
	ADDQ BX, DI
	ADDQ $16, DX
	DECQ CX
	JNZ   kh1g
	JMP   kh2
kh1z:
	LEAQ 12(R13), DI
	MOVQ  R11, CX
kh1zg:
	VMOVUPS X11, (DI)
	VMOVUPS X11, (DI)(R8*1)
	VMOVUPS X11, (DI)(R8*2)
	LEAQ (DI)(R8*2), AX
	VMOVUPS X11, (AX)(R8*1)
	ADDQ BX, DI
	DECQ CX
	JNZ   kh1zg

kh2:
	// kh = 2, column 9c+6. Last write into the channel block: use exact
	// 8+4-byte stores so no spill reaches the next channel/row.
	MOVQ 16(R10), DX
	LEAQ 24(R13), DI
	MOVQ  R11, CX
	TESTQ DX, DX
	JS    kh2z
	LEAQ (SI)(DX*4), DX
	ADDQ R15, DX
kh2g:
	VMOVUPS (DX), X0
	VMOVUPS 4(DX), X1
	VMOVUPS 8(DX), X2
	VUNPCKLPS X1, X0, X3
	VUNPCKHPS X1, X0, X4
	VUNPCKLPS X2, X2, X5
	VUNPCKHPS X2, X2, X6
	VSHUFPS $0x44, X5, X3, X7
	VSHUFPS $0xEE, X5, X3, X8
	VSHUFPS $0x44, X6, X4, X9
	VSHUFPS $0xEE, X6, X4, X10
	VMOVQ X7, (DI)
	VPSRLDQ $8, X7, X12
	VMOVD X12, 8(DI)
	VMOVQ X8, (DI)(R8*1)
	VPSRLDQ $8, X8, X12
	VMOVD X12, 8(DI)(R8*1)
	VMOVQ X9, (DI)(R8*2)
	VPSRLDQ $8, X9, X12
	VMOVD X12, 8(DI)(R8*2)
	LEAQ (DI)(R8*2), AX
	VMOVQ X10, (AX)(R8*1)
	VPSRLDQ $8, X10, X12
	VMOVD X12, 8(AX)(R8*1)
	ADDQ BX, DI
	ADDQ $16, DX
	DECQ CX
	JNZ   kh2g
	JMP   chNext
kh2z:
	LEAQ 24(R13), DI
	MOVQ  R11, CX
kh2zg:
	VMOVQ X11, (DI)
	VMOVD X11, 8(DI)
	VMOVQ X11, (DI)(R8*1)
	VMOVD X11, 8(DI)(R8*1)
	VMOVQ X11, (DI)(R8*2)
	VMOVD X11, 8(DI)(R8*2)
	LEAQ (DI)(R8*2), AX
	VMOVQ X11, (AX)(R8*1)
	VMOVD X11, 8(AX)(R8*1)
	ADDQ BX, DI
	DECQ CX
	JNZ   kh2zg

chNext:
	ADDQ $36, R13                              // next channel: col += 9
	ADDQ R9, SI                                // next channel plane
	DECQ R12
	JNZ   channel
	RET
