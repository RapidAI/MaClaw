//go:build arm64

#include "textflag.h"

// ARM64 NEON instruction encodings used in this file:
// FMUL  Vd.4S, Vn.4S, Vm.4S  = 0x6E20DC00 | Rm<<16 | Rn<<5 | Rd
// FMLA  Vd.4S, Vn.4S, Vm.4S  = 0x4E20CC00 | Rm<<16 | Rn<<5 | Rd
// SSHLL  Vd.8H, Vn.8B, #0    = 0x0F08A400 | Rn<<5 | Rd
// SSHLL2 Vd.8H, Vn.16B, #0   = 0x4F08A400 | Rn<<5 | Rd
// SSHLL  Vd.4S, Vn.4H, #0    = 0x0F10A400 | Rn<<5 | Rd
// SSHLL2 Vd.4S, Vn.8H, #0    = 0x4F10A400 | Rn<<5 | Rd
// SCVTF  Vd.4S, Vn.4S        = 0x4E21D800 | Rn<<5 | Rd
// FADDP  Vd.4S, Vn.4S, Vm.4S = 0x6E20D400 | Rm<<16 | Rn<<5 | Rd
// FADDP  Sd, Vn.2S            = 0x7E30D800 | Rn<<5 | Rd

// func dotQ8RowASM(a []float32, data []byte, rowOff, nBlocks int) float32
TEXT ·dotQ8RowASM(SB), NOSPLIT, $0-72
	MOVD  a+0(FP), R0         // R0 = &a[0]
	MOVD  data+24(FP), R1     // R1 = &data[0]
	MOVD  rowOff+48(FP), R2   // R2 = rowOff
	MOVD  nBlocks+56(FP), R3  // R3 = nBlocks
	ADD   R2, R1, R1          // R1 = &data[rowOff]

	VEOR  V0.B16, V0.B16, V0.B16 // V0 = accumulator = 0

	CBZ   R3, dot_done

dot_loop:
	// --- f16 scale → f32 → broadcast ---
	MOVHU (R1), R4
	UBFX  $10, R4, $5, R5     // exponent
	AND   $0x3ff, R4, R6      // mantissa
	CBZ   R5, dot_scale_zero
	ADD   $112, R5, R5
	LSL   $23, R5, R5
	LSL   $13, R6, R6
	ORR   R5, R6, R5
	FMOVS R5, F1
	B     dot_scale_ready
dot_scale_zero:
	FMOVS ZR, F1
dot_scale_ready:
	WORD  $0x4E040421            // DUP V1.4S, V1.S[0]  (broadcast scale)

	// Load 32 int8 values
	ADD   $2, R1, R8
	VLD1  (R8), [V2.B16, V3.B16]

	// --- Group 0: bytes[0..3] → V5.4S ---
	WORD  $0x0F08A444         // SSHLL V4.8H, V2.8B, #0
	WORD  $0x0F10A485         // SSHLL V5.4S, V4.4H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VLD1.P 16(R0), [V6.S4]
	WORD  $0x4E25CCC0         // FMLA V0.4S, V6.4S, V5.4S

	// --- Group 1: bytes[4..7] ---
	WORD  $0x4F10A485         // SSHLL2 V5.4S, V4.8H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VLD1.P 16(R0), [V6.S4]
	WORD  $0x4E25CCC0         // FMLA V0.4S, V6.4S, V5.4S

	// --- Group 2: bytes[8..11] ---
	WORD  $0x4F08A444         // SSHLL2 V4.8H, V2.16B, #0
	WORD  $0x0F10A485         // SSHLL V5.4S, V4.4H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VLD1.P 16(R0), [V6.S4]
	WORD  $0x4E25CCC0         // FMLA V0.4S, V6.4S, V5.4S

	// --- Group 3: bytes[12..15] ---
	WORD  $0x4F10A485         // SSHLL2 V5.4S, V4.8H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VLD1.P 16(R0), [V6.S4]
	WORD  $0x4E25CCC0         // FMLA V0.4S, V6.4S, V5.4S

	// --- Group 4: bytes[16..19] ---
	WORD  $0x0F08A464         // SSHLL V4.8H, V3.8B, #0
	WORD  $0x0F10A485         // SSHLL V5.4S, V4.4H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VLD1.P 16(R0), [V6.S4]
	WORD  $0x4E25CCC0         // FMLA V0.4S, V6.4S, V5.4S

	// --- Group 5: bytes[20..23] ---
	WORD  $0x4F10A485         // SSHLL2 V5.4S, V4.8H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VLD1.P 16(R0), [V6.S4]
	WORD  $0x4E25CCC0         // FMLA V0.4S, V6.4S, V5.4S

	// --- Group 6: bytes[24..27] ---
	WORD  $0x4F08A464         // SSHLL2 V4.8H, V3.16B, #0
	WORD  $0x0F10A485         // SSHLL V5.4S, V4.4H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VLD1.P 16(R0), [V6.S4]
	WORD  $0x4E25CCC0         // FMLA V0.4S, V6.4S, V5.4S

	// --- Group 7: bytes[28..31] ---
	WORD  $0x4F10A485         // SSHLL2 V5.4S, V4.8H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VLD1.P 16(R0), [V6.S4]
	WORD  $0x4E25CCC0         // FMLA V0.4S, V6.4S, V5.4S

	ADD   $34, R1, R1         // next Q8 block
	SUB   $1, R3, R3
	CBNZ  R3, dot_loop

dot_done:
	// Horizontal sum V0.4S → scalar
	WORD  $0x6E20D400         // FADDP V0.4S, V0.4S, V0.4S
	WORD  $0x7E30D800         // FADDP S0, V0.2S
	FMOVS F0, ret+64(FP)
	RET

// func dequantRowIntoASM(dst []float32, data []byte, rowOff, nBlocks int)
TEXT ·dequantRowIntoASM(SB), NOSPLIT, $0-64
	MOVD  dst+0(FP), R0
	MOVD  data+24(FP), R1
	MOVD  rowOff+48(FP), R2
	MOVD  nBlocks+56(FP), R3
	ADD   R2, R1, R1

	CBZ   R3, dq_done

dq_loop:
	MOVHU (R1), R4
	UBFX  $10, R4, $5, R5
	AND   $0x3ff, R4, R6
	CBZ   R5, dq_scale_zero
	ADD   $112, R5, R5
	LSL   $23, R5, R5
	LSL   $13, R6, R6
	ORR   R5, R6, R5
	FMOVS R5, F1
	B     dq_scale_ready
dq_scale_zero:
	FMOVS ZR, F1
dq_scale_ready:
	WORD  $0x4E040421            // DUP V1.4S, V1.S[0]

	ADD   $2, R1, R8
	VLD1  (R8), [V2.B16, V3.B16]

	// Group 0
	WORD  $0x0F08A444         // SSHLL V4.8H, V2.8B, #0
	WORD  $0x0F10A485         // SSHLL V5.4S, V4.4H, #0
	WORD  $0x4E21D8A5         // SCVTF V5.4S, V5.4S
	WORD  $0x6E25DC25         // FMUL V5.4S, V1.4S, V5.4S
	VST1.P [V5.S4], 16(R0)

	// Group 1
	WORD  $0x4F10A485
	WORD  $0x4E21D8A5
	WORD  $0x6E25DC25
	VST1.P [V5.S4], 16(R0)

	// Group 2
	WORD  $0x4F08A444
	WORD  $0x0F10A485
	WORD  $0x4E21D8A5
	WORD  $0x6E25DC25
	VST1.P [V5.S4], 16(R0)

	// Group 3
	WORD  $0x4F10A485
	WORD  $0x4E21D8A5
	WORD  $0x6E25DC25
	VST1.P [V5.S4], 16(R0)

	// Group 4
	WORD  $0x0F08A464
	WORD  $0x0F10A485
	WORD  $0x4E21D8A5
	WORD  $0x6E25DC25
	VST1.P [V5.S4], 16(R0)

	// Group 5
	WORD  $0x4F10A485
	WORD  $0x4E21D8A5
	WORD  $0x6E25DC25
	VST1.P [V5.S4], 16(R0)

	// Group 6
	WORD  $0x4F08A464
	WORD  $0x0F10A485
	WORD  $0x4E21D8A5
	WORD  $0x6E25DC25
	VST1.P [V5.S4], 16(R0)

	// Group 7
	WORD  $0x4F10A485
	WORD  $0x4E21D8A5
	WORD  $0x6E25DC25
	VST1.P [V5.S4], 16(R0)

	ADD   $34, R1, R1
	SUB   $1, R3, R3
	CBNZ  R3, dq_loop

dq_done:
	RET
