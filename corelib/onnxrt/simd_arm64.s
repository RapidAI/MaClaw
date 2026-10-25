//go:build arm64

#include "textflag.h"

// func fmadd3NEON(out, x *float32, w0, w1, w2 float32, n int)
// out[i] += w0*x[i] + w1*x[i+1] + w2*x[i+2], n a multiple of 4.
// Frame: out+0, x+8, w0+16, w1+20, w2+24, n+32
TEXT ·fmadd3NEON(SB), NOSPLIT, $0-40
	MOVD  out+0(FP), R0
	MOVD  x+8(FP), R1
	FMOVS w0+16(FP), F20
	FMOVS w1+20(FP), F21
	FMOVS w2+24(FP), F22
	MOVD  n+32(FP), R2
	VDUP  V20.S[0], V20.S4
	VDUP  V21.S[0], V21.S4
	VDUP  V22.S[0], V22.S4

f3_loop:
	CBZ   R2, f3_done
	VLD1  (R1), [V0.S4]
	ADD   $4, R1, R3
	VLD1  (R3), [V1.S4]
	ADD   $4, R3, R3
	VLD1  (R3), [V2.S4]
	VLD1  (R0), [V5.S4]
	WORD  $0x4E34CC05         // FMLA V5.4S, V0.4S, V20.4S
	WORD  $0x4E35CC25         // FMLA V5.4S, V1.4S, V21.4S
	WORD  $0x4E36CC45         // FMLA V5.4S, V2.4S, V22.4S
	VST1  [V5.S4], (R0)
	ADD   $16, R0, R0
	ADD   $16, R1, R1
	SUB   $4, R2, R2
	CBNZ  R2, f3_loop

f3_done:
	RET

// func fmadd5NEON(out, x *float32, w0, w1, w2, w3, w4 float32, n int)
// out[i] += w0*x[i] + ... + w4*x[i+4], n a multiple of 4.
// Frame: out+0, x+8, w0+16, w1+20, w2+24, w3+28, w4+32, n+40
TEXT ·fmadd5NEON(SB), NOSPLIT, $0-48
	MOVD  out+0(FP), R0
	MOVD  x+8(FP), R1
	FMOVS w0+16(FP), F20
	FMOVS w1+20(FP), F21
	FMOVS w2+24(FP), F22
	FMOVS w3+28(FP), F23
	FMOVS w4+32(FP), F24
	MOVD  n+40(FP), R2
	VDUP  V20.S[0], V20.S4
	VDUP  V21.S[0], V21.S4
	VDUP  V22.S[0], V22.S4
	VDUP  V23.S[0], V23.S4
	VDUP  V24.S[0], V24.S4

f5_loop:
	CBZ   R2, f5_done
	VLD1  (R1), [V0.S4]
	ADD   $4, R1, R3
	VLD1  (R3), [V1.S4]
	ADD   $4, R3, R3
	VLD1  (R3), [V2.S4]
	ADD   $4, R3, R3
	VLD1  (R3), [V3.S4]
	ADD   $4, R3, R3
	VLD1  (R3), [V4.S4]
	VLD1  (R0), [V5.S4]
	WORD  $0x4E34CC05         // FMLA V5.4S, V0.4S, V20.4S
	WORD  $0x4E35CC25         // FMLA V5.4S, V1.4S, V21.4S
	WORD  $0x4E36CC45         // FMLA V5.4S, V2.4S, V22.4S
	WORD  $0x4E37CC65         // FMLA V5.4S, V3.4S, V23.4S
	WORD  $0x4E38CC85         // FMLA V5.4S, V4.4S, V24.4S
	VST1  [V5.S4], (R0)
	ADD   $16, R0, R0
	ADD   $16, R1, R1
	SUB   $4, R2, R2
	CBNZ  R2, f5_loop

f5_done:
	RET

// func bilinear4NEON(out, a, b, c, d *float32, omx, wx, omy, wy float32, n int)
// Same product association as bilinear4AVX2. n a multiple of 4.
// An FMA with a zero addend is a multiply, so each (v*w1)*w2 matches the
// scalar gridBilinear rounding.
// Frame: out+0 a+8 b+16 c+24 d+32 omx+40 wx+44 omy+48 wy+52 n+56.
TEXT ·bilinear4NEON(SB), NOSPLIT, $0-64
	MOVD  out+0(FP), R0
	MOVD  a+8(FP), R1
	MOVD  b+16(FP), R2
	MOVD  c+24(FP), R3
	MOVD  d+32(FP), R4
	FMOVS omx+40(FP), F20
	FMOVS wx+44(FP), F21
	FMOVS omy+48(FP), F22
	FMOVS wy+52(FP), F23
	MOVD  n+56(FP), R5
	VDUP  V20.S[0], V20.S4
	VDUP  V21.S[0], V21.S4
	VDUP  V22.S[0], V22.S4
	VDUP  V23.S[0], V23.S4
	VEOR  V31.B16, V31.B16, V31.B16

b4n_loop:
	VLD1 (R1), [V0.S4]
	VLD1 (R2), [V1.S4]
	VLD1 (R3), [V2.S4]
	VLD1 (R4), [V3.S4]
	VEOR V31.B16, V31.B16, V4.B16
	VFMLA V0.S4, V20.S4, V4.S4    // a*(1-wx)
	VEOR V31.B16, V31.B16, V5.B16
	VFMLA V4.S4, V22.S4, V5.S4    // (a*(1-wx))*(1-wy)
	VEOR V31.B16, V31.B16, V4.B16
	VFMLA V1.S4, V21.S4, V4.S4
	VEOR V31.B16, V31.B16, V6.B16
	VFMLA V4.S4, V22.S4, V6.S4
	VADD V6.S4, V5.S4, V5.S4
	VEOR V31.B16, V31.B16, V4.B16
	VFMLA V2.S4, V20.S4, V4.S4
	VEOR V31.B16, V31.B16, V6.B16
	VFMLA V4.S4, V23.S4, V6.S4
	VADD V6.S4, V5.S4, V5.S4
	VEOR V31.B16, V31.B16, V4.B16
	VFMLA V3.S4, V21.S4, V4.S4
	VEOR V31.B16, V31.B16, V6.B16
	VFMLA V4.S4, V23.S4, V6.S4
	VADD V6.S4, V5.S4, V5.S4
	VST1 [V5.S4], (R0)
	ADD $16, R0, R0
	ADD $16, R1, R1
	ADD $16, R2, R2
	ADD $16, R3, R3
	ADD $16, R4, R4
	SUB $4, R5, R5
	CBNZ R5, b4n_loop
	RET

// func affineNEON(out, x *float32, scale, bias float32, n int)
// out[i] = x[i]*scale + bias. n a multiple of 4.
// Frame: out+0 x+8 scale+16 bias+20 n+24.
TEXT ·affineNEON(SB), NOSPLIT, $0-32
	MOVD  out+0(FP), R0
	MOVD  x+8(FP), R1
	FMOVS scale+16(FP), F20
	FMOVS bias+20(FP), F21
	MOVD  n+24(FP), R2
	VDUP  V20.S[0], V20.S4
	VDUP  V21.S[0], V21.S4
	VEOR  V31.B16, V31.B16, V31.B16

afn_loop:
	VLD1 (R1), [V0.S4]
	VEOR V31.B16, V31.B16, V1.B16
	VFMLA V0.S4, V20.S4, V1.S4    // x*scale
	VADD V21.S4, V1.S4, V1.S4     // + bias
	VST1 [V1.S4], (R0)
	ADD $16, R0, R0
	ADD $16, R1, R1
	SUB $4, R2, R2
	CBNZ R2, afn_loop
	RET

// func transpose4x4F32(dst *float32, ldDst int, src *float32, ldSrc int)
// dst[j*ldDst+i] = src[i*ldSrc+j] for a 4×4 block. Leading dimensions are
// in elements. Pure data movement.
// Frame: dst+0 ldDst+8 src+16 ldSrc+24.
TEXT ·transpose4x4F32(SB), NOSPLIT, $0-32
	MOVD dst+0(FP), R0
	MOVD ldDst+8(FP), R1
	LSL $2, R1, R1
	MOVD src+16(FP), R2
	MOVD ldSrc+24(FP), R3
	LSL $2, R3, R3
	VLD1 (R2), [V0.S4]
	ADD R3, R2, R2
	VLD1 (R2), [V1.S4]
	ADD R3, R2, R2
	VLD1 (R2), [V2.S4]
	ADD R3, R2, R2
	VLD1 (R2), [V3.S4]
	VTRN1 V1.S4, V0.S4, V4.S4
	VTRN2 V1.S4, V0.S4, V5.S4
	VTRN1 V3.S4, V2.S4, V6.S4
	VTRN2 V3.S4, V2.S4, V7.S4
	VTRN1 V6.D2, V4.D2, V0.D2
	VTRN2 V6.D2, V4.D2, V2.D2
	VTRN1 V7.D2, V5.D2, V1.D2
	VTRN2 V7.D2, V5.D2, V3.D2
	// V0=row0, V1=row1, V2=row2, V3=row3. trn2 of the even pair is row2,
	// not row1, so the rows must be stored in register order.
	VST1 [V0.S4], (R0)
	ADD R1, R0, R0
	VST1 [V1.S4], (R0)
	ADD R1, R0, R0
	VST1 [V2.S4], (R0)
	ADD R1, R0, R0
	VST1 [V3.S4], (R0)
	RET
