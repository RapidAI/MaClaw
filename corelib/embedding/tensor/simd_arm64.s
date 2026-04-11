//go:build arm64

#include "textflag.h"

// ARM64 NEON WORD encodings:
// FMUL  Vd.4S, Vn.4S, Vm.4S  = 0x6E20DC00 | Rm<<16 | Rn<<5 | Rd
// FSUB  Vd.4S, Vn.4S, Vm.4S  = 0x4EA0D400 | Rm<<16 | Rn<<5 | Rd
// FADD  Vd.4S, Vn.4S, Vm.4S  = 0x4E20D400 | Rm<<16 | Rn<<5 | Rd
// FMAX  Vd.4S, Vn.4S, Vm.4S  = 0x4E20F400 | Rm<<16 | Rn<<5 | Rd
// FMIN  Vd.4S, Vn.4S, Vm.4S  = 0x4EA0F400 | Rm<<16 | Rn<<5 | Rd
// FCVTZS Vd.4S, Vn.4S        = 0x4EA1B800 | Rn<<5 | Rd
// FRECPE Vd.4S, Vn.4S        = 0x4EA1D800 | Rn<<5 | Rd
// FRECPS Vd.4S, Vn.4S, Vm.4S = 0x4E20FC00 | Rm<<16 | Rn<<5 | Rd

// func siluMulASM(gate, up []float32)
TEXT ·siluMulASM(SB), NOSPLIT, $0-48
	MOVD  gate+0(FP), R0      // R0 = &gate[0]
	MOVD  gate+8(FP), R1      // R1 = len(gate)
	MOVD  up+24(FP), R2       // R2 = &up[0]

	CBZ   R1, silu_done

	// Load constants
	// V16 = A = 12102203.0 (0x4B38AA3B)
	MOVW  $0x4B38AA3B, R3
	FMOVS R3, F17
	WORD  $0x4E040630            // DUP V16.4S, V17.S[0]

	// V17 = B = 0x3F7F127F as int32
	MOVW  $0x3F7F127F, R3
	WORD  $0x4E040C71            // DUP V17.4S, W3

	// V18 = 1.0
	FMOVS $1.0, F19
	WORD  $0x4E040672            // DUP V18.4S, V19.S[0]

	// V19 = 0.0
	VEOR  V19.B16, V19.B16, V19.B16

	// V20 = -88.0 (0xC2B00000)
	MOVW  $0xC2B00000, R3
	FMOVS R3, F21
	WORD  $0x4E0406B4            // DUP V20.4S, V21.S[0]

	// V21 = 88.0 (0x42B00000)
	MOVW  $0x42B00000, R3
	FMOVS R3, F21
	WORD  $0x4E0406B5            // DUP V21.4S, V21.S[0]

	// Process 4 at a time
	MOVD  R1, R4
	LSR   $2, R4
	CBZ   R4, silu_tail

silu_loop4:
	VLD1  (R0), [V0.S4]       // V0 = v (don't advance yet)

	// V1 = 0 - v = -v
	// FSUB V1.4S, V19.4S, V0.4S: Vd=1, Vn=19, Vm=0
	// = 0x4EA0D400 | (0<<16) | (19<<5) | 1 = 0x4EA0D661
	WORD  $0x4EA0D661         // FSUB V1.4S, V19.4S, V0.4S  (V1 = -v)

	// Clamp: V1 = max(V1, V20) then min(V1, V21)
	// FMAX V1.4S, V1.4S, V20.4S: Vd=1, Vn=1, Vm=20
	// = 0x4E20F400 | (20<<16) | (1<<5) | 1 = 0x4E34F421
	WORD  $0x4E34F421         // FMAX V1.4S, V1.4S, V20.4S
	// FMIN V1.4S, V1.4S, V21.4S: Vd=1, Vn=1, Vm=21
	// = 0x4EA0F400 | (21<<16) | (1<<5) | 1 = 0x4EB5F421
	WORD  $0x4EB5F421         // FMIN V1.4S, V1.4S, V21.4S

	// exp(-v): int32(A * (-v)) + B
	// FMUL V2.4S, V16.4S, V1.4S: Vd=2, Vn=16, Vm=1
	// = 0x6E20DC00 | (1<<16) | (16<<5) | 2 = 0x6E21DE02
	WORD  $0x6E21DE02         // FMUL V2.4S, V16.4S, V1.4S
	// FCVTZS V2.4S, V2.4S
	WORD  $0x4EA1B842         // FCVTZS V2.4S, V2.4S
	// VADD (integer) V2 + V17
	VADD  V17.S4, V2.S4, V2.S4

	// 1 + exp(-v)
	// FADD V3.4S, V18.4S, V2.4S: Vd=3, Vn=18, Vm=2
	// = 0x4E20D400 | (2<<16) | (18<<5) | 3 = 0x4E22D643
	WORD  $0x4E22D643         // FADD V3.4S, V18.4S, V2.4S

	// v / (1+exp(-v)) via reciprocal estimate
	// FRECPE V4.4S, V3.4S
	WORD  $0x4EA1D864         // FRECPE V4.4S, V3.4S
	// FRECPS V5.4S, V3.4S, V4.4S: Vd=5, Vn=3, Vm=4
	// = 0x4E20FC00 | (4<<16) | (3<<5) | 5 = 0x4E24FC65
	WORD  $0x4E24FC65         // FRECPS V5.4S, V3.4S, V4.4S
	// FMUL V4.4S, V4.4S, V5.4S: Vd=4, Vn=4, Vm=5
	// = 0x6E20DC00 | (5<<16) | (4<<5) | 4 = 0x6E25DC84
	WORD  $0x6E25DC84         // FMUL V4.4S, V4.4S, V5.4S
	// Second Newton iteration
	WORD  $0x4E24FC65         // FRECPS V5.4S, V3.4S, V4.4S
	WORD  $0x6E25DC84         // FMUL V4.4S, V4.4S, V5.4S
	// V4 ≈ 1/(1+exp(-v))
	// SiLU = v * V4
	// FMUL V0.4S, V0.4S, V4.4S: Vd=0, Vn=0, Vm=4
	// = 0x6E20DC00 | (4<<16) | (0<<5) | 0 = 0x6E24DC00
	WORD  $0x6E24DC00         // FMUL V0.4S, V0.4S, V4.4S

	// * up
	VLD1.P 16(R2), [V1.S4]
	// FMUL V0.4S, V0.4S, V1.4S
	// = 0x6E20DC00 | (1<<16) | (0<<5) | 0 = 0x6E21DC00
	WORD  $0x6E21DC00         // FMUL V0.4S, V0.4S, V1.4S

	VST1.P [V0.S4], 16(R0)

	SUB   $1, R4, R4
	CBNZ  R4, silu_loop4

silu_tail:
	AND   $3, R1, R1
	CBZ   R1, silu_done

	MOVW  $0x3F7F127F, R5     // B

silu_scalar:
	FMOVS (R0), F0            // v
	FNEGS F0, F1              // -v
	MOVW  $0xC2B00000, R3
	FMOVS R3, F2
	FMAXS F2, F1, F1
	MOVW  $0x42B00000, R3
	FMOVS R3, F2
	FMINS F2, F1, F1
	// A * (-v)
	MOVW  $0x4B38AA3B, R3
	FMOVS R3, F16
	FMULS F16, F1, F2
	FCVTZSS F2, R3
	ADDW  R5, R3, R3
	FMOVS R3, F2
	FMOVS $1.0, F3
	FADDS F2, F3, F3
	FDIVS F3, F0, F0
	FMOVS (R2), F1
	FMULS F1, F0, F0
	FMOVS F0, (R0)
	ADD   $4, R0, R0
	ADD   $4, R2, R2
	SUB   $1, R1, R1
	CBNZ  R1, silu_scalar

silu_done:
	RET
