#!/usr/bin/env python3
"""Generate q8MultiDot4ScaledAVX2N64 + q8TripleMultiDot4ScaledAVX2N64 asm."""

from pathlib import Path


def single_block_half(data_base: int, a_base: int) -> str:
    lines = []
    for d0, d1, aoff in ((2, 6, 0), (10, 14, 32), (18, 22, 64), (26, 30, 96)):
        doff, doff2, a = data_base + d0, data_base + d1, a_base + aoff
        lines.append(f"\tVPMOVSXBD {doff}(DI), X4")
        lines.append(f"\tVPMOVSXBD {doff2}(DI), X5")
        lines.append("\tVINSERTI128 $1, X5, Y4, Y4")
        lines.append("\tVCVTDQ2PS Y4, Y4")
        lines.append("\tVMULPS Y15, Y4, Y4")
        for reg, yi in (("R8", 0), ("R9", 1), ("R10", 2), ("R12", 3)):
            mem = f"({reg})" if a == 0 else f"{a}({reg})"
            lines.append(f"\tVMOVUPS {mem}, Y5")
            lines.append(f"\tVFMADD231PS Y5, Y4, Y{yi}")
    return "\n".join(lines)


def triple_block(data_base: int, a_base: int, scale_off: int) -> str:
    lines = []
    for d0, d1, aoff in ((2, 6, 0), (10, 14, 32), (18, 22, 64), (26, 30, 96)):
        doff, doff2, a = data_base + d0, data_base + d1, a_base + aoff
        # B0
        if scale_off == 0:
            lines.append("\tMOVSS (R13), X15")
        else:
            lines.append(f"\tMOVSS {scale_off}(R13), X15")
        lines.append("\tVBROADCASTSS X15, Y15")
        lines.append(f"\tVPMOVSXBD {doff}(DI), X12")
        lines.append(f"\tVPMOVSXBD {doff2}(DI), X14")
        lines.append("\tVINSERTI128 $1, X14, Y12, Y12")
        lines.append("\tVCVTDQ2PS Y12, Y12")
        lines.append("\tVMULPS Y15, Y12, Y12")
        # B1
        if scale_off == 0:
            lines.append("\tMOVSS (R14), X15")
        else:
            lines.append(f"\tMOVSS {scale_off}(R14), X15")
        lines.append("\tVBROADCASTSS X15, Y15")
        lines.append(f"\tVPMOVSXBD {doff}(R15), X13")
        lines.append(f"\tVPMOVSXBD {doff2}(R15), X14")
        lines.append("\tVINSERTI128 $1, X14, Y13, Y13")
        lines.append("\tVCVTDQ2PS Y13, Y13")
        lines.append("\tVMULPS Y15, Y13, Y13")
        # B2 (reload scale after clobbering X15 with high half)
        if scale_off == 0:
            lines.append("\tMOVSS (DX), X15")
        else:
            lines.append(f"\tMOVSS {scale_off}(DX), X15")
        lines.append("\tVBROADCASTSS X15, Y15")
        lines.append(f"\tVPMOVSXBD {doff}(BX), X14")
        lines.append(f"\tVPMOVSXBD {doff2}(BX), X15")
        lines.append("\tVINSERTI128 $1, X15, Y14, Y14")
        lines.append("\tVCVTDQ2PS Y14, Y14")
        if scale_off == 0:
            lines.append("\tMOVSS (DX), X15")
        else:
            lines.append(f"\tMOVSS {scale_off}(DX), X15")
        lines.append("\tVBROADCASTSS X15, Y15")
        lines.append("\tVMULPS Y15, Y14, Y14")
        # Load each A row once into Y15, FMA into all 3 B accumulators.
        for reg, yb0, yb1, yb2 in (
            ("R8", 0, 4, 8),
            ("R9", 1, 5, 9),
            ("R10", 2, 6, 10),
            ("R12", 3, 7, 11),
        ):
            mem = f"({reg})" if a == 0 else f"{a}({reg})"
            lines.append(f"\tVMOVUPS {mem}, Y15")
            lines.append(f"\tVFMADD231PS Y15, Y12, Y{yb0}")
            lines.append(f"\tVFMADD231PS Y15, Y13, Y{yb1}")
            lines.append(f"\tVFMADD231PS Y15, Y14, Y{yb2}")
    return "\n".join(lines)


def hsum_store(n: int) -> str:
    lines = []
    for i in range(n):
        off = i * 4
        # use X12/X13 as temps — after loop, Y12/Y13 no longer hold B
        lines.append(f"\tVEXTRACTF128 $1, Y{i}, X12")
        lines.append(f"\tVADDPS X{i}, X12, X{i}")
        lines.append(f"\tVSHUFPD $1, X{i}, X{i}, X13")
        lines.append(f"\tVADDPS X{i}, X13, X{i}")
        lines.append(f"\tVMOVSHDUP X{i}, X13")
        lines.append(f"\tVADDSS X{i}, X13, X{i}")
        lines.append(f"\tMOVSS X{i}, {off}(R11)")
    return "\n".join(lines)


def main() -> None:
    single = f"""// func q8MultiDot4ScaledAVX2N64(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)
// nBlocks=64, K=2048 fixed. Row stride 8192. Dual-block body (32 iters).
// Frame: out+0 a+8 data+16 scales+24 rowOff+32 = 40
TEXT ·q8MultiDot4ScaledAVX2N64(SB), NOSPLIT, $0-40
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ data+16(FP), DI
	MOVQ scales+24(FP), R13
	ADDQ rowOff+32(FP), DI

	MOVQ SI, R8
	LEAQ 8192(SI), R9
	LEAQ 16384(SI), R10
	LEAQ 24576(SI), R12

	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3

	MOVQ $32, CX

md4s64_loop:
	PREFETCHT0 136(DI)
	// ── block 0 ──
	MOVSS (R13), X15
	VBROADCASTSS X15, Y15
{single_block_half(0, 0)}
	// ── block 1 (data+34, scale+4, A+128) ──
	MOVSS 4(R13), X15
	VBROADCASTSS X15, Y15
{single_block_half(34, 128)}
	ADDQ $68, DI
	ADDQ $8, R13
	ADDQ $256, R8
	ADDQ $256, R9
	ADDQ $256, R10
	ADDQ $256, R12
	DECQ CX
	JNZ  md4s64_loop

{hsum_store(4)}
	VZEROUPPER
	RET
"""

    triple = f"""// func q8TripleMultiDot4ScaledAVX2N64(out *[12]float32, a *float32, data *byte,
//   scales0, scales1, scales2 *float32, rowOff0, rowOff1, rowOff2 int)
// 4 A × 3 B, nBlocks=64, K=2048. A from memory once per chunk for all 3 B.
// out[0:4]=B0, out[4:8]=B1, out[8:12]=B2
// Frame: out+0 a+8 data+16 s0+24 s1+32 s2+40 off0+48 off1+56 off2+64 = 72
TEXT ·q8TripleMultiDot4ScaledAVX2N64(SB), NOSPLIT, $0-72
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ data+16(FP), DI
	MOVQ scales0+24(FP), R13
	MOVQ scales1+32(FP), R14
	MOVQ scales2+40(FP), DX
	ADDQ rowOff0+48(FP), DI
	MOVQ data+16(FP), R15
	ADDQ rowOff1+56(FP), R15
	MOVQ data+16(FP), BX
	ADDQ rowOff2+64(FP), BX

	MOVQ SI, R8
	LEAQ 8192(SI), R9
	LEAQ 16384(SI), R10
	LEAQ 24576(SI), R12

	VXORPS Y0, Y0, Y0
	VXORPS Y1, Y1, Y1
	VXORPS Y2, Y2, Y2
	VXORPS Y3, Y3, Y3
	VXORPS Y4, Y4, Y4
	VXORPS Y5, Y5, Y5
	VXORPS Y6, Y6, Y6
	VXORPS Y7, Y7, Y7
	VXORPS Y8, Y8, Y8
	VXORPS Y9, Y9, Y9
	VXORPS Y10, Y10, Y10
	VXORPS Y11, Y11, Y11

	MOVQ $32, CX

t64_block2:
	PREFETCHT0 136(DI)
	PREFETCHT0 136(R15)
	PREFETCHT0 136(BX)
	// ── block 0 ──
{triple_block(0, 0, 0)}
	// ── block 1 ──
{triple_block(34, 128, 4)}
	ADDQ $68, DI
	ADDQ $68, R15
	ADDQ $68, BX
	ADDQ $8, R13
	ADDQ $8, R14
	ADDQ $8, DX
	ADDQ $256, R8
	ADDQ $256, R9
	ADDQ $256, R10
	ADDQ $256, R12
	DECQ CX
	JNZ  t64_block2

{hsum_store(12)}
	VZEROUPPER
	RET
"""

    # Append after dual N64 (before q8MultiDot8AVX2)
    target = Path("corelib/embedding/tensor/q8_multidot_amd64.s")
    text = target.read_text(encoding="utf-8")
    marker = "// func q8MultiDot8AVX2(out *[8]float32, a *float32, K int, data *byte, rowOff, nBlocks int)"
    if "q8TripleMultiDot4ScaledAVX2N64" in text:
        print("already present, skip")
        return
    if marker not in text:
        raise SystemExit("marker not found")
    insert = "\n" + single + "\n" + triple + "\n"
    target.write_text(text.replace(marker, insert + marker, 1), encoding="utf-8")
    print("patched", target)


if __name__ == "__main__":
    main()
