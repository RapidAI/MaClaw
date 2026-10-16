#!/usr/bin/env python3
"""Generate AVX-512 multiDot + Q8 dual multiDot kernels for SenseVoice hot path."""

from pathlib import Path


def hsum_z_to_x28(zreg: int) -> str:
    """Horizontal sum Z[zreg] (16 floats) → X28 scalar. Temps Y28/Y29/X29.

    Go's assembler rejects VHADDPS on X16+ (no AVX-encoded form accepted for
    those encodings), so finish the 4-lane reduction with VSHUFPD/VMOVSHDUP.

    Uses Y{zreg} (low 256 of Z) + extract high half — saves one VEXTRACT vs
    extracting both halves into scratch regs.
    """
    return "\n".join([
        f"\tVEXTRACTF32X8 $1, Z{zreg}, Y28",
        f"\tVADDPS Y{zreg}, Y28, Y28",
        "\tVEXTRACTF32X4 $1, Y28, X29",
        "\tVADDPS X28, X29, X28",
        "\tVSHUFPD $1, X28, X28, X29",
        "\tVADDPS X28, X29, X28",
        "\tVMOVSHDUP X28, X29",
        "\tVADDSS X28, X29, X28",
    ])


def hsum_z_to_mem(zreg: int, mem: str) -> str:
    """Horizontal sum Z[zreg] → store float32 at mem."""
    return hsum_z_to_x28(zreg) + f"\n\tVMOVSS X28, {mem}"


def hsum_z_bias_relu_store(zreg: int, bn_x: str, zero_x: str, mem: str) -> str:
    """hsum(Z) + bn, max(0, ·), store to mem. bn_x/zero_x are XMM names like X26."""
    return "\n".join([
        hsum_z_to_x28(zreg),
        f"\tVADDSS {bn_x}, X28, X28",
        f"\tVMAXSS {zero_x}, X28, X28",
        f"\tVMOVSS X28, {mem}",
    ])


def hsum_z_bias_store(zreg: int, bn_x: str, mem: str) -> str:
    """hsum(Z) + bn, store to mem (encoder plain)."""
    return "\n".join([
        hsum_z_to_x28(zreg),
        f"\tVADDSS {bn_x}, X28, X28",
        f"\tVMOVSS X28, {mem}",
    ])


def _finish_x4_to_x28(xreg: int) -> str:
    """Horizontal sum of 4 floats in X{xreg} → scalar X28. Temp X29."""
    # If xreg is 28, operate in place with X29 only.
    if xreg == 28:
        return "\n".join([
            "\tVSHUFPD $1, X28, X28, X29",
            "\tVADDPS X28, X29, X28",
            "\tVMOVSHDUP X28, X29",
            "\tVADDSS X28, X29, X28",
        ])
    return "\n".join([
        f"\tVSHUFPD $1, X{xreg}, X{xreg}, X29",
        f"\tVADDPS X{xreg}, X29, X{xreg}",
        f"\tVMOVSHDUP X{xreg}, X29",
        f"\tVADDSS X{xreg}, X29, X28",
    ])


def hsum_batch_ops(items: list, kind: str = "mem") -> str:
    """Batch horizontal sums with parallel 16→4 reductions.

    items: list of dicts with keys:
      z: int Z register
      mem: store address (kind mem/bias/relu/accum)
      bn: optional XMM name for bias
      zero: optional XMM name for ReLU zero
    kind: 'mem' | 'bias' | 'relu' | 'accum'
      accum: load existing float from mem, add hsum+bn, store back
    Temps: Y24-Y27 for parallel partials (must not hold live accums).
    """
    lines = []
    # Process in groups of up to 4 for parallel extract/add.
    for i in range(0, len(items), 4):
        group = items[i : i + 4]
        ytemps = [24, 25, 26, 27]
        # Phase 1: 16 → 8 floats (parallel)
        for j, it in enumerate(group):
            yt = ytemps[j]
            z = it["z"]
            lines.append(f"\tVEXTRACTF32X8 $1, Z{z}, Y{yt}")
            lines.append(f"\tVADDPS Y{z}, Y{yt}, Y{yt}")
        # Phase 2: 8 → 4 floats (parallel)
        for j, it in enumerate(group):
            yt = ytemps[j]
            lines.append(f"\tVEXTRACTF32X4 $1, Y{yt}, X29")
            lines.append(f"\tVADDPS X{yt}, X29, X{yt}")
        # Phase 3: finish each 4-lane sum + post-op + store
        for j, it in enumerate(group):
            yt = ytemps[j]
            lines.append(_finish_x4_to_x28(yt))
            if kind == "bias":
                lines.append(f"\tVADDSS {it['bn']}, X28, X28")
                lines.append(f"\tVMOVSS X28, {it['mem']}")
            elif kind == "relu":
                lines.append(f"\tVADDSS {it['bn']}, X28, X28")
                lines.append(f"\tVMAXSS {it['zero']}, X28, X28")
                lines.append(f"\tVMOVSS X28, {it['mem']}")
            elif kind == "accum":
                lines.append(f"\tVADDSS {it['bn']}, X28, X28")
                lines.append(f"\tVMOVSS {it['mem']}, X29")
                lines.append("\tVADDSS X29, X28, X28")
                lines.append(f"\tVMOVSS X28, {it['mem']}")
            else:  # mem
                lines.append(f"\tVMOVSS X28, {it['mem']}")
    return "\n".join(lines)


# K=512 A row stride: 512*4 = 2048. SI = &A0[k].
_A_ROW_K512 = 2048


def _triple8_fma_rows(off: int, zb0: int, zb1: int, zb2: int, row0: int, nrows: int) -> str:
    """FMA nrows A rows starting at row0 against B in Z{zb0..zb2}.

    SI=&A0[k]; pair-load A into Z27/Z28. Accums: Zr=A·B0, Z{8+r}=A·B1, Z{16+r}=A·B2.
    """
    lines = []
    for p in range(row0, row0 + nrows, 2):
        o0 = p * _A_ROW_K512 + off
        o1 = (p + 1) * _A_ROW_K512 + off
        am0 = "(SI)" if o0 == 0 else f"{o0}(SI)"
        am1 = f"{o1}(SI)"
        lines.append(f"\tVMOVUPS {am0}, Z27")
        lines.append(f"\tVMOVUPS {am1}, Z28")
        lines.append(f"\tVFMADD231PS Z27, Z{zb0}, Z{p}")
        lines.append(f"\tVFMADD231PS Z27, Z{zb1}, Z{8+p}")
        lines.append(f"\tVFMADD231PS Z27, Z{zb2}, Z{16+p}")
        lines.append(f"\tVFMADD231PS Z28, Z{zb0}, Z{p+1}")
        lines.append(f"\tVFMADD231PS Z28, Z{zb1}, Z{8+p+1}")
        lines.append(f"\tVFMADD231PS Z28, Z{zb2}, Z{16+p+1}")
    return "\n".join(lines)


def _triple8_chunk(off: int) -> str:
    """One 16-float chunk: 3 B + 8 A, 24 FMAs (non-pipelined; used by callers if needed)."""
    if off == 0:
        b0, b1, b2 = "(DI)", "(R15)", "(R14)"
    else:
        b0, b1, b2 = f"{off}(DI)", f"{off}(R15)", f"{off}(R14)"
    lines = [
        f"\tVMOVUPS {b0}, Z24",
        f"\tVMOVUPS {b1}, Z25",
        f"\tVMOVUPS {b2}, Z26",
        _triple8_fma_rows(off, 24, 25, 26, 0, 8),
    ]
    return "\n".join(lines)


def _triple8_loop_body() -> str:
    """Two 16-float chunks (off=0,64) with dual-buffer B pipeline (v1).

    Load B@0 → Z24-26; FMA rows 0-3; load B@64 → Z29-31; finish rows 4-7 on B@0;
    then FMA all 8 on B@64. Hides B@64 load behind A FMA.
    A-pair half0/half1 interleave (v2/v3) measured neutral/worse under thermal —
    keep v1 until a clear cool win is shown.
    Prefetch: 3 B + 3 A T0 (rows 0,2,4) — was 4 A and stole BW on Zen4.
    """
    lines = [
        "\tPREFETCHT0 512(DI)",
        "\tPREFETCHT0 512(R15)",
        "\tPREFETCHT0 512(R14)",
        "\tPREFETCHT0 512(SI)",
        f"\tPREFETCHT0 {512 + 2 * _A_ROW_K512}(SI)",
        f"\tPREFETCHT0 {512 + 4 * _A_ROW_K512}(SI)",
        # B@0
        "\tVMOVUPS (DI), Z24",
        "\tVMOVUPS (R15), Z25",
        "\tVMOVUPS (R14), Z26",
        _triple8_fma_rows(0, 24, 25, 26, 0, 4),  # rows 0-3
        # Overlap next B load with remaining FMA on B@0
        "\tVMOVUPS 64(DI), Z29",
        "\tVMOVUPS 64(R15), Z30",
        "\tVMOVUPS 64(R14), Z31",
        _triple8_fma_rows(0, 24, 25, 26, 4, 4),  # rows 4-7 (nrows=4, not 8)
        # B@64 full 8 rows
        _triple8_fma_rows(64, 29, 30, 31, 0, 8),
        "\tADDQ $128, DI",
        "\tADDQ $128, R15",
        "\tADDQ $128, R14",
        "\tADDQ $128, SI",
    ]
    return "\n".join(lines)


def _triple8_k512_zero_and_loop(label: str) -> str:
    """Zero Z0-23, run 16 iters with R8 counter, SI=A base."""
    text = ""
    for i in range(24):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += f"""
	// 32×16-float chunks / 2 = 16 iters; SI=&A0[k], R8=counter
	MOVQ $16, R8
{label}:
"""
    text += _triple8_loop_body() + "\n"
    text += f"\tDECQ R8\n\tJNZ  {label}\n"
    return text


def gen_multidot8_triple_k512() -> str:
    # 8 A × 3 B, one pass over K. Accums:
    # Z0-Z7  : A0..A7 · B0
    # Z8-Z15 : A0..A7 · B1
    # Z16-Z23: A0..A7 · B2
    # Z24=B0, Z25=B1, Z26=B2; A loaded via mem-FMA (no Z27 temp)
    # GP: R11=out0, BX=out1, SI=counter, DI=b0, R15=b1, R14=b2
    # A bases: R8 R9 R10 R12 R13 CX DX AX

    mapping = [
        (0, "(R11)"), (1, "4(R11)"), (2, "8(R11)"), (3, "12(R11)"),
        (8, "16(R11)"), (9, "20(R11)"), (10, "24(R11)"), (11, "28(R11)"),
        (16, "32(R11)"), (17, "36(R11)"), (18, "40(R11)"), (19, "44(R11)"),
        (4, "(BX)"), (5, "4(BX)"), (6, "8(BX)"), (7, "12(BX)"),
        (12, "16(BX)"), (13, "20(BX)"), (14, "24(BX)"), (15, "28(BX)"),
        (20, "32(BX)"), (21, "36(BX)"), (22, "40(BX)"), (23, "44(BX)"),
    ]
    stores = [hsum_batch_ops([{"z": z, "mem": mem} for z, mem in mapping], "mem")]

    text = """// func multiDot8TripleBAVX512K512(out0, out1 *[12]float32, a, b0, b1, b2 *float32)
// 8 A × 3 B, K=512, one pass over B (AVX-512). Single SI A-base (r*2048).
// out0/out1 layout matches multiDot4Triple: [0:4]=B0, [4:8]=B1, [8:12]=B2.
// Frame: out0+0 out1+8 a+16 b0+24 b1+32 b2+40 → 48
TEXT ·multiDot8TripleBAVX512K512(SB), NOSPLIT, $0-48
	MOVQ out0+0(FP), R11
	MOVQ out1+8(FP), BX
	MOVQ a+16(FP), SI
	MOVQ b0+24(FP), DI
	MOVQ b1+32(FP), R15
	MOVQ b2+40(FP), R14

"""
    text += _triple8_k512_zero_and_loop("tri8_512_loop") + "\n"
    text += "\n".join(stores) + "\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_triple_relu_k512_n2048() -> str:
    """FFN up: 8A×3B K=512 + bias + ReLU store for N=2048.

    out points to &out[0]; writes rows m..m+7, cols n..n+2.
    Same FMA body as multiDot8Triple; fused max(0, hsum+bn) store.
    """
    # Frame: out+0 a+8 b0+16 b1+24 b2+32 m+40 n+48 bn0+56 bn1+60 bn2+64 → 68
    # Stack $8: save out base (m*2048+n)*4
    # Bias read from FP each time so Y24-Y27 batch temps don't clobber bn regs.
    items = []
    for r in range(4):
        off = r * 8192
        items.append({"z": r, "mem": f"{off}(R11)", "bn": "bn0+56(FP)", "zero": "X31"})
        items.append({"z": 8 + r, "mem": f"{off+4}(R11)", "bn": "bn1+60(FP)", "zero": "X31"})
        items.append({"z": 16 + r, "mem": f"{off+8}(R11)", "bn": "bn2+64(FP)", "zero": "X31"})
    for r in range(4):
        off = (4 + r) * 8192
        items.append({"z": 4 + r, "mem": f"{off}(R11)", "bn": "bn0+56(FP)", "zero": "X31"})
        items.append({"z": 12 + r, "mem": f"{off+4}(R11)", "bn": "bn1+60(FP)", "zero": "X31"})
        items.append({"z": 20 + r, "mem": f"{off+8}(R11)", "bn": "bn2+64(FP)", "zero": "X31"})
    stores = [
        "\tMOVQ 0(SP), R11",
        "\tVXORPS X31, X31, X31",
        hsum_batch_ops(items, "relu"),
    ]

    text = """// func multiDot8TripleReLUAVX512K512N2048(out *float32, a, b0, b1, b2 *float32, m, n int, bn0, bn1, bn2 float32)
// FFN up fused: 8A×3B K=512 + bias + ReLU into out[m:m+8, n:n+3], N=2048.
// Frame: out+0 a+8 b0+16 b1+24 b2+32 m+40 n+48 bn0+56 bn1+60 bn2+64 → 68
// Stack $8: out base address.
TEXT ·multiDot8TripleReLUAVX512K512N2048(SB), NOSPLIT, $8-68
	MOVQ out+0(FP), R11
	MOVQ m+40(FP), AX
	MOVQ n+48(FP), BX
	// base = (m*2048 + n) * 4
	SHLQ $11, AX // m*2048
	ADDQ BX, AX
	SHLQ $2, AX
	ADDQ AX, R11
	MOVQ R11, 0(SP)

	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
	MOVQ b2+32(FP), R14

"""
    text += _triple8_k512_zero_and_loop("tri8_relu_loop") + "\n"
    text += "\n".join(stores) + "\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_triple_argmax_k512() -> str:
    """CTC: 8A×3B K=512 + bias + in-place argmax into bestV/bestI (len 8).

    Two-phase post-FMA to respect Go asm limits (UCOMISS only on X0-X15)
    without clobbering live Z0-23 accums:
      1) hsum each Z + bias → stack[24] floats (uses only X28/X29)
      2) compare stack vs bestV with UCOMISS X0,X1 and update
    """
    # Frame: bestV+0 bestI+8 a+16 b0+24 b1+32 b2+40 n+48 bn0+56 bn1+60 bn2+64 → 68
    # Stack $96: 24 float32 hsums
    # Layout stack[r*3+c] = row r, col c  (r=0..7, c=0..2)
    z_for = []
    for r in range(8):
        if r < 4:
            z_for.append((r, 8 + r, 16 + r))
        else:
            rr = r - 4
            z_for.append((4 + rr, 12 + rr, 20 + rr))

    # Phase1: batch hsum+bias → stack (parallel 16→4 reductions).
    p1_items = []
    for r, (z0, z1, z2) in enumerate(z_for):
        for z, c, bn in ((z0, 0, "bn0+56(FP)"), (z1, 1, "bn1+60(FP)"), (z2, 2, "bn2+64(FP)")):
            p1_items.append({"z": z, "mem": f"{(r * 3 + c) * 4}(SP)", "bn": bn})
    phase1 = hsum_batch_ops(p1_items, "bias")

    # Phase2 v2: keep bestV[r] in X0..X7; among 3 cands pick max+idx first,
    # then one compare vs best (1 bestV load/store per row vs 3).
    phase2 = []
    phase2.append("\t// Load bestV[0..7] into X0..X7")
    for r in range(8):
        phase2.append(f"\tVMOVSS {r * 4}(R11), X{r}")
    for r in range(8):
        lab3 = f"amx_r{r}c"
        # X8 = running cand max, DX = running cand col offset 0..2
        s0, s1, s2 = r * 12, r * 12 + 4, r * 12 + 8
        phase2.append(f"\t// row {r}: max of 3 cands")
        phase2.append(f"\tVMOVSS {s0}(SP), X8")
        phase2.append("\tXORQ DX, DX")  # col 0
        phase2.append(f"\tVMOVSS {s1}(SP), X9")
        phase2.append("\tUCOMISS X8, X9")
        phase2.append(f"\tJLS  {lab3}a")
        phase2.append("\tVMOVAPS X9, X8")
        phase2.append("\tMOVQ $1, DX")
        phase2.append(f"{lab3}a:")
        phase2.append(f"\tVMOVSS {s2}(SP), X9")
        phase2.append("\tUCOMISS X8, X9")
        phase2.append(f"\tJLS  {lab3}b")
        phase2.append("\tVMOVAPS X9, X8")
        phase2.append("\tMOVQ $2, DX")
        phase2.append(f"{lab3}b:")
        # X8=candMax, DX=col; compare to best in X{r}
        phase2.append(f"\tUCOMISS X{r}, X8")
        phase2.append(f"\tJLS  {lab3}")
        phase2.append(f"\tVMOVAPS X8, X{r}")
        phase2.append("\tLEAQ (SI)(DX*1), R9")  # n+col
        phase2.append(f"\tMOVQ R9, {r * 8}(BX)")
        phase2.append(f"{lab3}:")
    phase2.append("\t// Write back bestV")
    for r in range(8):
        phase2.append(f"\tVMOVSS X{r}, {r * 4}(R11)")

    text = """// func multiDot8TripleArgmaxAVX512K512(bestV *float32, bestI *int, a, b0, b1, b2 *float32, n int, bn0, bn1, bn2 float32)
// CTC fused: 8A×3B K=512 + bias + argmax update of bestV/bestI (8 rows).
// Frame: bestV+0 bestI+8 a+16 b0+24 b1+32 b2+40 n+48 bn0+56 bn1+60 bn2+64 → 68
// Stack $96: 24 hsum+bias; phase2 keeps bestV in X0-X7.
TEXT ·multiDot8TripleArgmaxAVX512K512(SB), NOSPLIT, $96-68
	MOVQ a+16(FP), SI
	MOVQ b0+24(FP), DI
	MOVQ b1+32(FP), R15
	MOVQ b2+40(FP), R14

"""
    text += _triple8_k512_zero_and_loop("tri8_amx_loop") + "\n"
    text += phase1 + "\n"
    text += """
	MOVQ bestV+0(FP), R11
	MOVQ bestI+8(FP), BX
	MOVQ n+48(FP), SI

"""
    text += "\n".join(phase2) + "\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_triple_plain_k512(n_cols: int) -> str:
    """Encoder plain: 8A×3B K=512 + bias store for fixed N (512 or 1536).

    out writes rows m..m+7, cols n..n+2. No ReLU/accum.
    """
    row_bytes = n_cols * 4
    # m*N: 512→SHLQ $9; 1536→IMUL $1536
    if n_cols == 512:
        base_calc = "\tSHLQ $9, AX // m*512\n"
        label = "N512"
        loop = "tri8_pl512_loop"
    elif n_cols == 1536:
        # 1536 = 3 * 512. On Zen4 this LEA+shift sequence has lower latency
        # than the immediate IMUL on this per-call address-generation path.
        base_calc = "\tLEAQ (AX)(AX*2), AX // m*3\n\tSHLQ $9, AX // m*1536\n"
        label = "N1536"
        loop = "tri8_pl1536_loop"
    else:
        raise ValueError(f"unsupported N={n_cols}")

    items = []
    for r in range(4):
        off = r * row_bytes
        items.append({"z": r, "mem": f"{off}(R11)", "bn": "bn0+56(FP)"})
        items.append({"z": 8 + r, "mem": f"{off+4}(R11)", "bn": "bn1+60(FP)"})
        items.append({"z": 16 + r, "mem": f"{off+8}(R11)", "bn": "bn2+64(FP)"})
    for r in range(4):
        off = (4 + r) * row_bytes
        items.append({"z": 4 + r, "mem": f"{off}(R11)", "bn": "bn0+56(FP)"})
        items.append({"z": 12 + r, "mem": f"{off+4}(R11)", "bn": "bn1+60(FP)"})
        items.append({"z": 20 + r, "mem": f"{off+8}(R11)", "bn": "bn2+64(FP)"})
    stores = [
        "\tMOVQ 0(SP), R11",
        hsum_batch_ops(items, "bias"),
    ]

    text = f"""// func multiDot8TriplePlainAVX512K512{label}(out *float32, a, b0, b1, b2 *float32, m, n int, bn0, bn1, bn2 float32)
// Encoder fused: 8A×3B K=512 + bias into out[m:m+8, n:n+3], N={n_cols}.
// Frame: out+0 a+8 b0+16 b1+24 b2+32 m+40 n+48 bn0+56 bn1+60 bn2+64 → 68
// Stack $8: out base address.
TEXT ·multiDot8TriplePlainAVX512K512{label}(SB), NOSPLIT, $8-68
	MOVQ out+0(FP), R11
	MOVQ m+40(FP), AX
	MOVQ n+48(FP), BX
	// base = (m*{n_cols} + n) * 4
{base_calc}	ADDQ BX, AX
	SHLQ $2, AX
	ADDQ AX, R11
	MOVQ R11, 0(SP)

	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
	MOVQ b2+32(FP), R14

"""
    text += _triple8_k512_zero_and_loop(loop) + "\n"
    text += "\n".join(stores) + "\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot4_triple_k512() -> str:
    # 4 A × 3 B ZMM - for rows=4 path
    # Z0-Z3 B0, Z4-Z7 B1, Z8-Z11 B2
    # Z12=A, Z13-Z15=B
    # A: R8 R9 R10 R12, B: DI R15 R14, out: R11, counter: R13
    def chunk(off: int) -> str:
        if off == 0:
            b = ["(DI)", "(R15)", "(R14)"]
            a = ["(R8)", "(R9)", "(R10)", "(R12)"]
        else:
            b = [f"{off}(DI)", f"{off}(R15)", f"{off}(R14)"]
            a = [f"{off}(R8)", f"{off}(R9)", f"{off}(R10)", f"{off}(R12)"]
        lines = [
            f"\tVMOVUPS {b[0]}, Z13",
            f"\tVMOVUPS {b[1]}, Z14",
            f"\tVMOVUPS {b[2]}, Z15",
        ]
        for i, ar in enumerate(a):
            lines.append(f"\tVMOVUPS {ar}, Z12")
            lines.append(f"\tVFMADD231PS Z12, Z13, Z{i}")
            lines.append(f"\tVFMADD231PS Z12, Z14, Z{4+i}")
            lines.append(f"\tVFMADD231PS Z12, Z15, Z{8+i}")
        return "\n".join(lines)

    stores = []
    for i in range(12):
        stores.append(hsum_z_to_mem(i, f"{i*4}(R11)"))

    text = """// func multiDot4TripleBAVX512K512(out *[12]float32, a, b0, b1, b2 *float32)
// 4 A × 3 B, K=512, AVX-512 16-wide FMA.
// Frame: out+0 a+8 b0+16 b1+24 b2+32 → 40
TEXT ·multiDot4TripleBAVX512K512(SB), NOSPLIT, $0-40
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
	MOVQ b2+32(FP), R14

	MOVQ SI, R8
	LEAQ 2048(SI), R9
	LEAQ 4096(SI), R10
	LEAQ 6144(SI), R12

"""
    for i in range(12):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $16, R13
tri4_512_loop:
	PREFETCHT0 512(DI)
	PREFETCHT0 512(R15)
	PREFETCHT0 512(R14)
	PREFETCHT0 512(R8)
"""
    text += chunk(0) + "\n" + chunk(64) + "\n"
    text += """	ADDQ $128, DI
	ADDQ $128, R15
	ADDQ $128, R14
	ADDQ $128, R8
	ADDQ $128, R9
	ADDQ $128, R10
	ADDQ $128, R12
	DECQ R13
	JNZ  tri4_512_loop

"""
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot4_dual_k512() -> str:
    def chunk(off: int) -> str:
        if off == 0:
            b0, b1 = "(DI)", "(R15)"
            a = ["(R8)", "(R9)", "(R10)", "(R12)"]
        else:
            b0, b1 = f"{off}(DI)", f"{off}(R15)"
            a = [f"{off}(R8)", f"{off}(R9)", f"{off}(R10)", f"{off}(R12)"]
        lines = [
            f"\tVMOVUPS {b0}, Z8",
            f"\tVMOVUPS {b1}, Z9",
        ]
        for i, ar in enumerate(a):
            lines.append(f"\tVMOVUPS {ar}, Z10")
            lines.append(f"\tVFMADD231PS Z10, Z8, Z{i}")
            lines.append(f"\tVFMADD231PS Z10, Z9, Z{4+i}")
        return "\n".join(lines)

    stores = []
    for i in range(8):
        stores.append(hsum_z_to_mem(i, f"{i*4}(R11)"))

    text = """// func multiDot4DualBAVX512K512(out *[8]float32, a, b0, b1 *float32)
// 4 A × 2 B, K=512, AVX-512.
// Frame: out+0 a+8 b0+16 b1+24 → 32
TEXT ·multiDot4DualBAVX512K512(SB), NOSPLIT, $0-32
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15

	MOVQ SI, R8
	LEAQ 2048(SI), R9
	LEAQ 4096(SI), R10
	LEAQ 6144(SI), R12

"""
    for i in range(8):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $16, R13
dual4_512_loop:
	PREFETCHT0 512(DI)
	PREFETCHT0 512(R15)
	PREFETCHT0 512(R8)
"""
    text += chunk(0) + "\n" + chunk(64) + "\n"
    text += """	ADDQ $128, DI
	ADDQ $128, R15
	ADDQ $128, R8
	ADDQ $128, R9
	ADDQ $128, R10
	ADDQ $128, R12
	DECQ R13
	JNZ  dual4_512_loop

"""
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8_dual_n64() -> str:
    """q8DualMultiDot4ScaledAVX512N64: 4 A × 2 B, K=2048, nBlocks=64.
    Process 16 floats (half Q8 block) at a time with ZMM.
    Accums Z0-Z3 · B0, Z4-Z7 · B1
    A stride 8192
    """
    # One half-block at data_off relative to DI/R15, A at a_off, scale already in Z14/Z15
    def half_block(data_off: int, a_off: int) -> str:
        # int8 at data_off+2 relative to block start; for second half data_off includes +16
        # VPMOVSXBD zmm, m128 expands 16 bytes
        d0 = f"{data_off}(DI)" if data_off else "(DI)"
        d1 = f"{data_off}(R15)" if data_off else "(R15)"
        # Actually data_off is offset to first int8 of the 16
        lines = [
            f"\tVPMOVSXBD {data_off}(DI), Z8",
            "\tVCVTDQ2PS Z8, Z8",
            "\tVMULPS Z14, Z8, Z8",
            f"\tVPMOVSXBD {data_off}(R15), Z9",
            "\tVCVTDQ2PS Z9, Z9",
            "\tVMULPS Z15, Z9, Z9",
        ]
        a_regs = ["R8", "R9", "R10", "R12"]
        for i, reg in enumerate(a_regs):
            if a_off == 0:
                am = f"({reg})"
            else:
                am = f"{a_off}({reg})"
            lines.append(f"\tVMOVUPS {am}, Z10")
            lines.append(f"\tVFMADD231PS Z10, Z8, Z{i}")
            lines.append(f"\tVFMADD231PS Z10, Z9, Z{4+i}")
        return "\n".join(lines)

    # Full Q8 block: scale at R13/R14, int8 at DI+2 and DI+18, second block DI+34
    def full_block(block_byte_off: int, scale_off: int, a_base: int) -> str:
        # DI points to start of block pair stream; block 0 at 0, block 1 at 34
        # int8 first 16 at block+2, second 16 at block+18
        if scale_off == 0:
            sc0, sc1 = "(R13)", "(R14)"
        else:
            sc0, sc1 = f"{scale_off}(R13)", f"{scale_off}(R14)"
        d_base = block_byte_off
        lines = [
            f"\tMOVSS {sc0}, X14",
            "\tVBROADCASTSS X14, Z14",
            f"\tMOVSS {sc1}, X15",
            "\tVBROADCASTSS X15, Z15",
            half_block(d_base + 2, a_base),
            half_block(d_base + 18, a_base + 64),
        ]
        return "\n".join(lines)

    stores = []
    for i in range(8):
        stores.append(hsum_z_to_mem(i, f"{i*4}(R11)"))

    text = """// func q8DualMultiDot4ScaledAVX512N64(out *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)
// nBlocks=64, K=2048. AVX-512: 16-wide dequant+FMA per half Q8 block.
// Frame: out+0 a+8 data+16 s0+24 s1+32 off0+40 off1+48 → 56
TEXT ·q8DualMultiDot4ScaledAVX512N64(SB), NOSPLIT, $0-56
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ data+16(FP), DI
	MOVQ scales0+24(FP), R13
	MOVQ scales1+32(FP), R14
	ADDQ rowOff0+40(FP), DI
	MOVQ data+16(FP), R15
	ADDQ rowOff1+48(FP), R15

	MOVQ SI, R8
	LEAQ 8192(SI), R9
	LEAQ 16384(SI), R10
	LEAQ 24576(SI), R12

"""
    for i in range(8):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	// 64 blocks / 2 = 32 dual-block iters
	MOVQ $32, CX

n64_avx512_loop:
	PREFETCHT0 272(DI)
	PREFETCHT0 272(R15)
	PREFETCHT0 256(R8)
	PREFETCHT0 256(R9)
"""
    # block 0 at 0, A at 0; block 1 at 34, A at 128
    text += full_block(0, 0, 0) + "\n"
    text += full_block(34, 4, 128) + "\n"
    text += """	ADDQ $68, DI
	ADDQ $68, R15
	ADDQ $8, R13
	ADDQ $8, R14
	ADDQ $256, R8
	ADDQ $256, R9
	ADDQ $256, R10
	ADDQ $256, R12
	DECQ CX
	JNZ  n64_avx512_loop

"""
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot4_dual_k128() -> str:
    """4 A × 2 B, K=128 (attention). 16-wide → 8 chunks, unroll ×2 → 4 iters."""
    def chunk(off: int) -> str:
        if off == 0:
            b0, b1 = "(DI)", "(R15)"
            a = ["(R8)", "(R9)", "(R10)", "(R12)"]
        else:
            b0, b1 = f"{off}(DI)", f"{off}(R15)"
            a = [f"{off}(R8)", f"{off}(R9)", f"{off}(R10)", f"{off}(R12)"]
        lines = [f"\tVMOVUPS {b0}, Z8", f"\tVMOVUPS {b1}, Z9"]
        for i, ar in enumerate(a):
            lines.append(f"\tVMOVUPS {ar}, Z10")
            lines.append(f"\tVFMADD231PS Z10, Z8, Z{i}")
            lines.append(f"\tVFMADD231PS Z10, Z9, Z{4+i}")
        return "\n".join(lines)

    stores = "\n".join(hsum_z_to_mem(i, f"{i*4}(R11)") for i in range(8))
    text = """// func multiDot4DualBAVX512K128(out *[8]float32, a, b0, b1 *float32)
// 4 A × 2 B, K=128 attention headDim.
// Frame: out+0 a+8 b0+16 b1+24 → 32
TEXT ·multiDot4DualBAVX512K128(SB), NOSPLIT, $0-32
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
	// stride 128*4 = 512
	MOVQ SI, R8
	LEAQ 512(SI), R9
	LEAQ 1024(SI), R10
	LEAQ 1536(SI), R12
"""
    for i in range(8):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $4, R13
dual4_128_loop:
"""
    text += chunk(0) + "\n" + chunk(64) + "\n"
    text += """	ADDQ $128, DI
	ADDQ $128, R15
	ADDQ $128, R8
	ADDQ $128, R9
	ADDQ $128, R10
	ADDQ $128, R12
	DECQ R13
	JNZ  dual4_128_loop

"""
    text += stores + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot4_triple_k128() -> str:
    def chunk(off: int) -> str:
        if off == 0:
            b = ["(DI)", "(R15)", "(R14)"]
            a = ["(R8)", "(R9)", "(R10)", "(R12)"]
        else:
            b = [f"{off}(DI)", f"{off}(R15)", f"{off}(R14)"]
            a = [f"{off}(R8)", f"{off}(R9)", f"{off}(R10)", f"{off}(R12)"]
        lines = [
            f"\tVMOVUPS {b[0]}, Z13",
            f"\tVMOVUPS {b[1]}, Z14",
            f"\tVMOVUPS {b[2]}, Z15",
        ]
        for i, ar in enumerate(a):
            lines.append(f"\tVMOVUPS {ar}, Z12")
            lines.append(f"\tVFMADD231PS Z12, Z13, Z{i}")
            lines.append(f"\tVFMADD231PS Z12, Z14, Z{4+i}")
            lines.append(f"\tVFMADD231PS Z12, Z15, Z{8+i}")
        return "\n".join(lines)

    stores = "\n".join(hsum_z_to_mem(i, f"{i*4}(R11)") for i in range(12))
    text = """// func multiDot4TripleBAVX512K128(out *[12]float32, a, b0, b1, b2 *float32)
// 4 A × 3 B, K=128 attention.
// Frame: out+0 a+8 b0+16 b1+24 b2+32 → 40
TEXT ·multiDot4TripleBAVX512K128(SB), NOSPLIT, $0-40
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
	MOVQ b2+32(FP), R14
	MOVQ SI, R8
	LEAQ 512(SI), R9
	LEAQ 1024(SI), R10
	LEAQ 1536(SI), R12
"""
    for i in range(12):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $4, R13
tri4_128_loop:
"""
    text += chunk(0) + "\n" + chunk(64) + "\n"
    text += """	ADDQ $128, DI
	ADDQ $128, R15
	ADDQ $128, R14
	ADDQ $128, R8
	ADDQ $128, R9
	ADDQ $128, R10
	ADDQ $128, R12
	DECQ R13
	JNZ  tri4_128_loop

"""
    text += stores + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_dual_k128() -> str:
    """8 A × 2 B one-pass, K=128 attention. 16 ZMM accums."""
    # Z0-Z7 · B0, Z8-Z15 · B1; Z16=B0, Z17=B1, Z18=A
    a_regs = ["R8", "R9", "R10", "R12", "R13", "CX", "DX", "AX"]

    def chunk(off: int) -> str:
        if off == 0:
            b0, b1 = "(DI)", "(R15)"
            def am(r):
                return f"({r})"
        else:
            b0, b1 = f"{off}(DI)", f"{off}(R15)"
            def am(r):
                return f"{off}({r})"
        lines = [f"\tVMOVUPS {b0}, Z16", f"\tVMOVUPS {b1}, Z17"]
        for i, reg in enumerate(a_regs):
            lines.append(f"\tVMOVUPS {am(reg)}, Z18")
            lines.append(f"\tVFMADD231PS Z18, Z16, Z{i}")
            lines.append(f"\tVFMADD231PS Z18, Z17, Z{8+i}")
        return "\n".join(lines)

    # out0 gets A0-3: Z0-Z3 → B0, Z8-Z11 → B1
    # out1 gets A4-7: Z4-Z7 → B0, Z12-Z15 → B1
    stores = []
    for i in range(4):
        stores.append(hsum_z_to_mem(i, f"{i*4}(R11)"))  # out0 B0
    for i in range(4):
        stores.append(hsum_z_to_mem(8 + i, f"{16+i*4}(R11)"))  # out0 B1
    for i in range(4):
        stores.append(hsum_z_to_mem(4 + i, f"{i*4}(BX)"))  # out1 B0
    for i in range(4):
        stores.append(hsum_z_to_mem(12 + i, f"{16+i*4}(BX)"))  # out1 B1

    text = """// func multiDot8DualBAVX512K128(out0, out1 *[8]float32, a, b0, b1 *float32)
// 8 A × 2 B, K=128, one B pass (attention multiDot8DualB).
// Frame: out0+0 out1+8 a+16 b0+24 b1+32 → 40
TEXT ·multiDot8DualBAVX512K128(SB), NOSPLIT, $0-40
	MOVQ out0+0(FP), R11
	MOVQ out1+8(FP), BX
	MOVQ a+16(FP), SI
	MOVQ b0+24(FP), DI
	MOVQ b1+32(FP), R15
	// A strides 512
	MOVQ SI, R8
	LEAQ 512(SI), R9
	LEAQ 1024(SI), R10
	LEAQ 1536(SI), R12
	LEAQ 2048(SI), R13
	LEAQ 2560(SI), CX
	LEAQ 3072(SI), DX
	LEAQ 3584(SI), AX
"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $4, SI
dual8_128_loop:
"""
    text += chunk(0) + "\n" + chunk(64) + "\n"
    text += "\tADDQ $128, DI\n\tADDQ $128, R15\n"
    for r in a_regs:
        text += f"\tADDQ $128, {r}\n"
    text += "\tDECQ SI\n\tJNZ  dual8_128_loop\n\n"
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_triple_k128() -> str:
    """8 A × 3 B one-pass K=128 (attention). Single SI A-base, stride 512."""
    a_stride = 512  # 128*4

    def chunk(off: int) -> str:
        if off == 0:
            b0, b1, b2 = "(DI)", "(R15)", "(R14)"
        else:
            b0, b1, b2 = f"{off}(DI)", f"{off}(R15)", f"{off}(R14)"
        lines = [
            f"\tVMOVUPS {b0}, Z24",
            f"\tVMOVUPS {b1}, Z25",
            f"\tVMOVUPS {b2}, Z26",
        ]
        for p in range(0, 8, 2):
            o0 = p * a_stride + off
            o1 = (p + 1) * a_stride + off
            am0 = "(SI)" if o0 == 0 else f"{o0}(SI)"
            am1 = f"{o1}(SI)"
            lines.append(f"\tVMOVUPS {am0}, Z27")
            lines.append(f"\tVMOVUPS {am1}, Z28")
            lines.append(f"\tVFMADD231PS Z27, Z24, Z{p}")
            lines.append(f"\tVFMADD231PS Z27, Z25, Z{8+p}")
            lines.append(f"\tVFMADD231PS Z27, Z26, Z{16+p}")
            lines.append(f"\tVFMADD231PS Z28, Z24, Z{p+1}")
            lines.append(f"\tVFMADD231PS Z28, Z25, Z{8+p+1}")
            lines.append(f"\tVFMADD231PS Z28, Z26, Z{16+p+1}")
        return "\n".join(lines)

    store_items = (
        [{"z": i, "mem": f"{i*4}(R11)"} for i in range(4)]
        + [{"z": 8 + i, "mem": f"{16+i*4}(R11)"} for i in range(4)]
        + [{"z": 16 + i, "mem": f"{32+i*4}(R11)"} for i in range(4)]
        + [{"z": 4 + i, "mem": f"{i*4}(BX)"} for i in range(4)]
        + [{"z": 12 + i, "mem": f"{16+i*4}(BX)"} for i in range(4)]
        + [{"z": 20 + i, "mem": f"{32+i*4}(BX)"} for i in range(4)]
    )
    stores = [hsum_batch_ops(store_items, "mem")]

    text = """// func multiDot8TripleBAVX512K128(out0, out1 *[12]float32, a, b0, b1, b2 *float32)
// 8 A × 3 B, K=128 attention, one B pass. SI=&A0[k], row stride 512.
// Frame: out0+0 out1+8 a+16 b0+24 b1+32 b2+40 → 48
TEXT ·multiDot8TripleBAVX512K128(SB), NOSPLIT, $0-48
	MOVQ out0+0(FP), R11
	MOVQ out1+8(FP), BX
	MOVQ a+16(FP), SI
	MOVQ b0+24(FP), DI
	MOVQ b1+32(FP), R15
	MOVQ b2+40(FP), R14
"""
    for i in range(24):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	// K=128: 8 chunks of 16 floats / 2 = 4 iters
	MOVQ $4, R8
tri8_128_loop:
	PREFETCHT0 256(DI)
	PREFETCHT0 256(R15)
	PREFETCHT0 256(R14)
	PREFETCHT0 256(SI)
"""
    text += chunk(0) + "\n" + chunk(64) + "\n"
    text += """	ADDQ $128, DI
	ADDQ $128, R15
	ADDQ $128, R14
	ADDQ $128, SI
	DECQ R8
	JNZ  tri8_128_loop

"""
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot4_k512() -> str:
    """4 A × 1 B, K=512."""
    def chunk(off: int) -> str:
        if off == 0:
            b, a = "(DI)", ["(R8)", "(R9)", "(R10)", "(R12)"]
        else:
            b = f"{off}(DI)"
            a = [f"{off}(R8)", f"{off}(R9)", f"{off}(R10)", f"{off}(R12)"]
        lines = [f"\tVMOVUPS {b}, Z4"]
        for i, ar in enumerate(a):
            lines.append(f"\tVMOVUPS {ar}, Z5")
            lines.append(f"\tVFMADD231PS Z5, Z4, Z{i}")
        return "\n".join(lines)

    stores = "\n".join(hsum_z_to_mem(i, f"{i*4}(R11)") for i in range(4))
    text = """// func multiDot4AVX512K512(out *[4]float32, a, b *float32)
// Frame: out+0 a+8 b+16 → 24
TEXT ·multiDot4AVX512K512(SB), NOSPLIT, $0-24
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b+16(FP), DI
	MOVQ SI, R8
	LEAQ 2048(SI), R9
	LEAQ 4096(SI), R10
	LEAQ 6144(SI), R12
	VXORPS Z0, Z0, Z0
	VXORPS Z1, Z1, Z1
	VXORPS Z2, Z2, Z2
	VXORPS Z3, Z3, Z3
	MOVQ $16, R13
md4_512_loop:
"""
    text += chunk(0) + "\n" + chunk(64) + "\n"
    text += """	ADDQ $128, DI
	ADDQ $128, R8
	ADDQ $128, R9
	ADDQ $128, R10
	ADDQ $128, R12
	DECQ R13
	JNZ  md4_512_loop

"""
    text += stores + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8_single_n64() -> str:
    """q8MultiDot4ScaledAVX512N64: 4 A × 1 B."""
    def half(data_off: int, a_off: int) -> str:
        lines = [
            f"\tVPMOVSXBD {data_off}(DI), Z4",
            "\tVCVTDQ2PS Z4, Z4",
            "\tVMULPS Z15, Z4, Z4",
        ]
        for i, reg in enumerate(["R8", "R9", "R10", "R12"]):
            am = f"({reg})" if a_off == 0 else f"{a_off}({reg})"
            lines.append(f"\tVMOVUPS {am}, Z5")
            lines.append(f"\tVFMADD231PS Z5, Z4, Z{i}")
        return "\n".join(lines)

    def block(boff: int, soff: int, a_base: int) -> str:
        sc = "(R13)" if soff == 0 else f"{soff}(R13)"
        return "\n".join([
            f"\tMOVSS {sc}, X15",
            "\tVBROADCASTSS X15, Z15",
            half(boff + 2, a_base),
            half(boff + 18, a_base + 64),
        ])

    stores = "\n".join(hsum_z_to_mem(i, f"{i*4}(R11)") for i in range(4))
    text = """// func q8MultiDot4ScaledAVX512N64(out *[4]float32, a *float32, data *byte, scales *float32, rowOff int)
// Frame: out+0 a+8 data+16 scales+24 rowOff+32 → 40
TEXT ·q8MultiDot4ScaledAVX512N64(SB), NOSPLIT, $0-40
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ data+16(FP), DI
	MOVQ scales+24(FP), R13
	ADDQ rowOff+32(FP), DI
	MOVQ SI, R8
	LEAQ 8192(SI), R9
	LEAQ 16384(SI), R10
	LEAQ 24576(SI), R12
	VXORPS Z0, Z0, Z0
	VXORPS Z1, Z1, Z1
	VXORPS Z2, Z2, Z2
	VXORPS Z3, Z3, Z3
	MOVQ $32, CX
qn64_loop:
	PREFETCHT0 272(DI)
"""
    text += block(0, 0, 0) + "\n" + block(34, 4, 128) + "\n"
    text += """	ADDQ $68, DI
	ADDQ $8, R13
	ADDQ $256, R8
	ADDQ $256, R9
	ADDQ $256, R10
	ADDQ $256, R12
	DECQ CX
	JNZ  qn64_loop

"""
    text += stores + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_dequant_single() -> str:
    """dequantRowScaledAVX512: one Q8 row → float, f32 scales, 16-wide."""
    # Frame matches AVX2: dst+0(24) data+24(24) scales+48 rowOff+56 nBlocks+64 → 72
    def block_one(boff: int, dst_base: int, soff: int) -> str:
        sc = "(R8)" if soff == 0 else f"{soff}(R8)"
        return f"""	VBROADCASTSS {sc}, Z1
	VPMOVSXBD {boff+2}(DI), Z2
	VPMOVSXBD {boff+18}(DI), Z3
	VCVTDQ2PS Z2, Z2
	VCVTDQ2PS Z3, Z3
	VMULPS Z1, Z2, Z2
	VMULPS Z1, Z3, Z3
	VMOVUPS Z2, {dst_base}(SI)
	VMOVUPS Z3, {dst_base+64}(SI)
"""

    text = """// func dequantRowScaledAVX512(dst []float32, data []byte, scales *float32, rowOff, nBlocks int)
// Single-row Q8→f32 with f32 scale cache. Frame matches AVX2 (72 bytes).
TEXT ·dequantRowScaledAVX512(SB), NOSPLIT, $0-72
	MOVQ dst+0(FP), SI
	MOVQ data+24(FP), DI
	MOVQ scales+48(FP), R8
	ADDQ rowOff+56(FP), DI
	MOVQ nBlocks+64(FP), CX
	TESTQ CX, CX
	JZ   dqs1_512_done

	MOVQ CX, R10
	SHRQ $1, R10
	TESTQ R10, R10
	JZ   dqs1_512_one

dqs1_512_loop2:
	PREFETCHT0 136(DI)
	PREFETCHT0 256(SI)
"""
    text += block_one(0, 0, 0)
    text += block_one(34, 128, 4)
    text += """	ADDQ $68, DI
	ADDQ $8, R8
	ADDQ $256, SI
	DECQ R10
	JNZ  dqs1_512_loop2

	MOVQ nBlocks+64(FP), CX
	ANDQ $1, CX
	JZ   dqs1_512_done

dqs1_512_one:
	TESTQ CX, CX
	JZ   dqs1_512_done
dqs1_512_one_loop:
"""
    text += block_one(0, 0, 0)
    text += """	ADDQ $34, DI
	ADDQ $4, R8
	ADDQ $128, SI
	DECQ CX
	JNZ  dqs1_512_one_loop

dqs1_512_done:
	VZEROUPPER
	RET
"""
    return text


def gen_dequant_dual() -> str:
    """dequantRowScaledDualAVX512: two rows, dual-block, general nBlocks (even preferred)."""
    # Frame same as AVX2 version for drop-in: slice headers
    # dst0+0(24) dst1+24(24) data+48(24) s0+72 s1+80 off0+88 off1+96 nBlocks+104 = 112

    def block_pair(boff: int, dst_base: int, soff: int) -> str:
        """One Q8 block (32 floats): both halves, both rows, ILP-friendly."""
        sc0 = "(R8)" if soff == 0 else f"{soff}(R8)"
        sc1 = "(R9)" if soff == 0 else f"{soff}(R9)"
        return f"""	VBROADCASTSS {sc0}, Z1
	VBROADCASTSS {sc1}, Z4
	VPMOVSXBD {boff+2}(DI), Z2
	VPMOVSXBD {boff+2}(R15), Z3
	VPMOVSXBD {boff+18}(DI), Z5
	VPMOVSXBD {boff+18}(R15), Z6
	VCVTDQ2PS Z2, Z2
	VCVTDQ2PS Z3, Z3
	VCVTDQ2PS Z5, Z5
	VCVTDQ2PS Z6, Z6
	VMULPS Z1, Z2, Z2
	VMULPS Z4, Z3, Z3
	VMULPS Z1, Z5, Z5
	VMULPS Z4, Z6, Z6
	VMOVUPS Z2, {dst_base}(SI)
	VMOVUPS Z3, {dst_base}(R10)
	VMOVUPS Z5, {dst_base+64}(SI)
	VMOVUPS Z6, {dst_base+64}(R10)
"""

    text = """// func dequantRowScaledDualAVX512(dst0, dst1 []float32, data []byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)
// Frame identical to dequantRowScaledDualAVX2 (112 bytes).
TEXT ·dequantRowScaledDualAVX512(SB), NOSPLIT, $0-112
	MOVQ dst0+0(FP), SI
	MOVQ dst1+24(FP), R10
	MOVQ data+48(FP), DI
	MOVQ scales0+72(FP), R8
	MOVQ scales1+80(FP), R9
	ADDQ rowOff0+88(FP), DI
	MOVQ data+48(FP), R15
	ADDQ rowOff1+96(FP), R15
	MOVQ nBlocks+104(FP), CX
	TESTQ CX, CX
	JZ   dqs512_done

	// dual-block when even
	MOVQ CX, R11
	SHRQ $1, R11
	TESTQ R11, R11
	JZ   dqs512_one

dqs512_loop2:
	PREFETCHT0 204(DI)
	PREFETCHT0 204(R15)
	PREFETCHT0 256(SI)
	PREFETCHT0 256(R10)
"""
    text += block_pair(0, 0, 0)
    text += block_pair(34, 128, 4)
    text += """	ADDQ $68, DI
	ADDQ $68, R15
	ADDQ $8, R8
	ADDQ $8, R9
	ADDQ $256, SI
	ADDQ $256, R10
	DECQ R11
	JNZ  dqs512_loop2

	MOVQ nBlocks+104(FP), CX
	ANDQ $1, CX
	JZ   dqs512_done

dqs512_one:
	TESTQ CX, CX
	JZ   dqs512_done
dqs512_one_loop:
"""
    text += block_pair(0, 0, 0)
    text += """	ADDQ $34, DI
	ADDQ $34, R15
	ADDQ $4, R8
	ADDQ $4, R9
	ADDQ $128, SI
	ADDQ $128, R10
	DECQ CX
	JNZ  dqs512_one_loop

dqs512_done:
	VZEROUPPER
	RET
"""
    return text


def gen_dequant_triple() -> str:
    """dequantRowScaledTripleAVX512: 3 B rows (triple multiDot panel fill)."""
    # dst0+0(24) dst1+24(24) dst2+48(24) data+72(24) s0+96 s1+104 s2+112
    # off0+120 off1+128 off2+136 nBlocks+144 → 152
    def block_one(boff: int, dst_base: int, soff: int) -> str:
        """Pipelined: load half1 while storing half0 (separate Z regs).
        Note: load-all-then-cvt measured slower (dequant share ↑) — keep overlap.
        """
        sc = []
        for i in range(3):
            sm = f"({['R8','R9','R11'][i]})" if soff == 0 else f"{soff}({['R8','R9','R11'][i]})"
            sc.append(f"\tVBROADCASTSS {sm}, Z{[1, 4, 7][i]}")
        # half0 → Z2,Z3,Z5; half1 → Z8,Z9,Z10
        lines = sc + [
            f"\tVPMOVSXBD {boff + 2}(DI), Z2",
            f"\tVPMOVSXBD {boff + 2}(R15), Z3",
            f"\tVPMOVSXBD {boff + 2}(R13), Z5",
            "\tVCVTDQ2PS Z2, Z2",
            "\tVCVTDQ2PS Z3, Z3",
            "\tVCVTDQ2PS Z5, Z5",
            "\tVMULPS Z1, Z2, Z2",
            "\tVMULPS Z4, Z3, Z3",
            "\tVMULPS Z7, Z5, Z5",
            # Overlap half1 expand with half0 store
            f"\tVPMOVSXBD {boff + 18}(DI), Z8",
            f"\tVPMOVSXBD {boff + 18}(R15), Z9",
            f"\tVPMOVSXBD {boff + 18}(R13), Z10",
            f"\tVMOVUPS Z2, {dst_base}(SI)",
            f"\tVMOVUPS Z3, {dst_base}(R10)",
            f"\tVMOVUPS Z5, {dst_base}(R12)",
            "\tVCVTDQ2PS Z8, Z8",
            "\tVCVTDQ2PS Z9, Z9",
            "\tVCVTDQ2PS Z10, Z10",
            "\tVMULPS Z1, Z8, Z8",
            "\tVMULPS Z4, Z9, Z9",
            "\tVMULPS Z7, Z10, Z10",
            f"\tVMOVUPS Z8, {dst_base + 64}(SI)",
            f"\tVMOVUPS Z9, {dst_base + 64}(R10)",
            f"\tVMOVUPS Z10, {dst_base + 64}(R12)",
        ]
        return "\n".join(lines)

    text = """// func dequantRowScaledTripleAVX512(dst0, dst1, dst2 []float32, data []byte, scales0, scales1, scales2 *float32, rowOff0, rowOff1, rowOff2, nBlocks int)
// Three Q8 rows → float panels (triple multiDot feed). Frame 152 bytes.
// 4-block main (K=512 → 4 iters). 8-block body measured L1I thrash (share ↑).
// load-all-halves-then-cvt measured slower than half0-store||half1-expand.
TEXT ·dequantRowScaledTripleAVX512(SB), NOSPLIT, $0-152
	MOVQ dst0+0(FP), SI
	MOVQ dst1+24(FP), R10
	MOVQ dst2+48(FP), R12
	MOVQ data+72(FP), DI
	MOVQ scales0+96(FP), R8
	MOVQ scales1+104(FP), R9
	MOVQ scales2+112(FP), R11
	ADDQ rowOff0+120(FP), DI
	MOVQ data+72(FP), R15
	ADDQ rowOff1+128(FP), R15
	MOVQ data+72(FP), R13
	ADDQ rowOff2+136(FP), R13
	MOVQ nBlocks+144(FP), CX
	TESTQ CX, CX
	JZ   dqt512_done

	// 4-block main loop (K=512 → 16 blocks → 4 iters)
	MOVQ CX, BX
	SHRQ $2, BX
	TESTQ BX, BX
	JZ   dqt512_rem2

dqt512_loop4:
	PREFETCHT0 272(DI)
	PREFETCHT0 272(R15)
	PREFETCHT0 272(R13)
	// dst panel write stream (multiDot reads soon; T0 keeps L1 warm for stores)
	PREFETCHT0 512(SI)
	PREFETCHT0 512(R10)
	PREFETCHT0 512(R12)
"""
    text += block_one(0, 0, 0) + "\n"
    text += block_one(34, 128, 4) + "\n"
    text += block_one(68, 256, 8) + "\n"
    text += block_one(102, 384, 12) + "\n"
    text += """	ADDQ $136, DI
	ADDQ $136, R15
	ADDQ $136, R13
	ADDQ $16, R8
	ADDQ $16, R9
	ADDQ $16, R11
	ADDQ $512, SI
	ADDQ $512, R10
	ADDQ $512, R12
	DECQ BX
	JNZ  dqt512_loop4

dqt512_rem2:
	MOVQ nBlocks+144(FP), CX
	ANDQ $3, CX
	JZ   dqt512_done
	// remaining 1–3 blocks
dqt512_one_loop:
"""
    text += block_one(0, 0, 0) + "\n"
    text += """	ADDQ $34, DI
	ADDQ $34, R15
	ADDQ $34, R13
	ADDQ $4, R8
	ADDQ $4, R9
	ADDQ $4, R11
	ADDQ $128, SI
	ADDQ $128, R10
	ADDQ $128, R12
	DECQ CX
	JNZ  dqt512_one_loop

dqt512_done:
	VZEROUPPER
	RET
"""
    return text


# A row stride for K=2048: 2048*4 = 8192 bytes. Single SI base at A0[k].
_A_ROW = 8192


def _q8_dual8_half(data_off: int, a_off: int) -> str:
    """Dequant 16 Q8 of B0/B1 + pair-FMA against 8 A rows.

    Accums: Z0-3 A0-3·B0, Z4-7 A0-3·B1, Z8-11 A4-7·B0, Z12-15 A4-7·B1.
    Scales Z30/Z31. SI=&A0[k]. Pair-load A (Z18/Z19) — measured better than
    holding all 8 A live (Zen4 rename/port balance).
    """
    lines = [
        f"\tVPMOVSXBD {data_off}(DI), Z16",
        "\tVCVTDQ2PS Z16, Z16",
        "\tVMULPS Z30, Z16, Z16",
        f"\tVPMOVSXBD {data_off}(R15), Z17",
        "\tVCVTDQ2PS Z17, Z17",
        "\tVMULPS Z31, Z17, Z17",
    ]
    for i in range(0, 4, 2):
        o0 = i * _A_ROW + a_off
        o1 = (i + 1) * _A_ROW + a_off
        am0 = "(SI)" if o0 == 0 else f"{o0}(SI)"
        am1 = f"{o1}(SI)"
        lines.append(f"\tVMOVUPS {am0}, Z18")
        lines.append(f"\tVMOVUPS {am1}, Z19")
        lines.append(f"\tVFMADD231PS Z18, Z16, Z{i}")
        lines.append(f"\tVFMADD231PS Z18, Z17, Z{4+i}")
        lines.append(f"\tVFMADD231PS Z19, Z16, Z{i+1}")
        lines.append(f"\tVFMADD231PS Z19, Z17, Z{4+i+1}")
    for i in range(0, 4, 2):
        o0 = (4 + i) * _A_ROW + a_off
        o1 = (5 + i) * _A_ROW + a_off
        lines.append(f"\tVMOVUPS {o0}(SI), Z18")
        lines.append(f"\tVMOVUPS {o1}(SI), Z19")
        lines.append(f"\tVFMADD231PS Z18, Z16, Z{8+i}")
        lines.append(f"\tVFMADD231PS Z18, Z17, Z{12+i}")
        lines.append(f"\tVFMADD231PS Z19, Z16, Z{8+i+1}")
        lines.append(f"\tVFMADD231PS Z19, Z17, Z{12+i+1}")
    return "\n".join(lines)


def _q8_dual8_dequant_half(data_off: int, z0: int, z1: int) -> str:
    """Dequant 16 int8 of B0/B1 into Z{z0}/Z{z1} with scales Z30/Z31."""
    return "\n".join([
        f"\tVPMOVSXBD {data_off}(DI), Z{z0}",
        f"\tVCVTDQ2PS Z{z0}, Z{z0}",
        f"\tVMULPS Z30, Z{z0}, Z{z0}",
        f"\tVPMOVSXBD {data_off}(R15), Z{z1}",
        f"\tVCVTDQ2PS Z{z1}, Z{z1}",
        f"\tVMULPS Z31, Z{z1}, Z{z1}",
    ])


def _q8_dual8_fma_half(a_off: int, zb0: int, zb1: int) -> str:
    """Pair-FMA 8 A rows against B0/B1 in Z{zb0}/Z{zb1}."""
    lines = []
    for i in range(0, 4, 2):
        o0 = i * _A_ROW + a_off
        o1 = (i + 1) * _A_ROW + a_off
        am0 = "(SI)" if o0 == 0 else f"{o0}(SI)"
        am1 = f"{o1}(SI)"
        lines.append(f"\tVMOVUPS {am0}, Z18")
        lines.append(f"\tVMOVUPS {am1}, Z19")
        lines.append(f"\tVFMADD231PS Z18, Z{zb0}, Z{i}")
        lines.append(f"\tVFMADD231PS Z18, Z{zb1}, Z{4+i}")
        lines.append(f"\tVFMADD231PS Z19, Z{zb0}, Z{i+1}")
        lines.append(f"\tVFMADD231PS Z19, Z{zb1}, Z{4+i+1}")
    for i in range(0, 4, 2):
        o0 = (4 + i) * _A_ROW + a_off
        o1 = (5 + i) * _A_ROW + a_off
        lines.append(f"\tVMOVUPS {o0}(SI), Z18")
        lines.append(f"\tVMOVUPS {o1}(SI), Z19")
        lines.append(f"\tVFMADD231PS Z18, Z{zb0}, Z{8+i}")
        lines.append(f"\tVFMADD231PS Z18, Z{zb1}, Z{12+i}")
        lines.append(f"\tVFMADD231PS Z19, Z{zb0}, Z{8+i+1}")
        lines.append(f"\tVFMADD231PS Z19, Z{zb1}, Z{12+i+1}")
    return "\n".join(lines)


def _q8_dual8_fma_rows(a_off: int, zb0: int, zb1: int, row0: int) -> str:
    """Pair-FMA 4 consecutive A rows starting at row0 (0 or 4)."""
    lines = []
    for i in range(0, 4, 2):
        r = row0 + i
        o0 = r * _A_ROW + a_off
        o1 = (r + 1) * _A_ROW + a_off
        am0 = "(SI)" if o0 == 0 else f"{o0}(SI)"
        am1 = f"{o1}(SI)"
        # Accums: rows0-3 B0=Z0-3 B1=Z4-7; rows4-7 B0=Z8-11 B1=Z12-15
        if row0 < 4:
            z_b0, z_b1 = r, 4 + r
        else:
            rr = r - 4
            z_b0, z_b1 = 8 + rr, 12 + rr
        lines.append(f"\tVMOVUPS {am0}, Z18")
        lines.append(f"\tVMOVUPS {am1}, Z19")
        lines.append(f"\tVFMADD231PS Z18, Z{zb0}, Z{z_b0}")
        lines.append(f"\tVFMADD231PS Z18, Z{zb1}, Z{z_b1}")
        lines.append(f"\tVFMADD231PS Z19, Z{zb0}, Z{z_b0 + 1}")
        lines.append(f"\tVFMADD231PS Z19, Z{zb1}, Z{z_b1 + 1}")
    return "\n".join(lines)


def _q8_dual8_block(boff: int, soff: int, a_base: int) -> str:
    """One Q8 block (32 K): pipelined dequant/FMA.

    half0 → Z16/Z17, half1 → Z20/Z21. Dequant half1 starts after only
    4-row FMA of half0 (not full 8-row), hiding int8→f32 behind more FMA.
    """
    sc0 = "(AX)" if soff == 0 else f"{soff}(AX)"
    sc1 = "(BX)" if soff == 0 else f"{soff}(BX)"
    return "\n".join([
        f"\tVBROADCASTSS {sc0}, Z30",
        f"\tVBROADCASTSS {sc1}, Z31",
        _q8_dual8_dequant_half(boff + 2, 16, 17),
        _q8_dual8_fma_rows(a_base, 16, 17, 0),
        _q8_dual8_dequant_half(boff + 18, 20, 21),
        _q8_dual8_fma_rows(a_base, 16, 17, 4),
        _q8_dual8_fma_half(a_base + 64, 20, 21),
    ])


def _q8_dual8_loop_advance() -> str:
    """Advance after 2 Q8 blocks (64 K floats). One A pointer only."""
    return """	ADDQ $68, DI
	ADDQ $68, R15
	ADDQ $8, AX
	ADDQ $8, BX
	ADDQ $256, SI
"""


def _q8_dual8_loop_body(label_suffix: str = "") -> str:
    """2 Q8 blocks per iter (64 K floats). Single SI A-base + deep prefetch."""
    lines = [
        "\tPREFETCHT0 408(DI)",
        "\tPREFETCHT0 408(R15)",
        "\tPREFETCHT0 384(SI)",
        f"\tPREFETCHT0 {384 + _A_ROW}(SI)",
        f"\tPREFETCHT0 {384 + 2 * _A_ROW}(SI)",
        f"\tPREFETCHT0 {384 + 4 * _A_ROW}(SI)",
        _q8_dual8_block(0, 0, 0),
        _q8_dual8_block(34, 4, 128),
        _q8_dual8_loop_advance().rstrip(),
    ]
    return "\n".join(lines)


def gen_q8_dual8_n64() -> str:
    """q8DualMultiDot8ScaledAVX512N64: 8 A × 2 B one-pass, K=2048.
    Accum layout matches dual-4 exactly (so storeDual4Accum is happy):
      out0: Z0-3=A0-3·B0, Z4-7=A0-3·B1
      out1: Z8-11=A4-7·B0, Z12-15=A4-7·B1
    """
    store_items = [{"z": i, "mem": f"{i*4}(R11)"} for i in range(8)]
    store_items += [{"z": 8 + i, "mem": f"{i*4}(R10)"} for i in range(8)]
    stores = [
        "\tMOVQ 0(SP), R11",  # out0
        "\tMOVQ 8(SP), R10",  # out1
        hsum_batch_ops(store_items, "mem"),
    ]

    text = """// func q8DualMultiDot8ScaledAVX512N64(out0, out1 *[8]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int)
// 8 A × 2 B, K=2048, ONE pass over Q8 B (FFN down hot path).
// Frame: out0+0 out1+8 a+16 data+24 s0+32 s1+40 off0+48 off1+56 → 64
// Stack $16: save out0/out1 (need GPRs for 8 A bases).
TEXT ·q8DualMultiDot8ScaledAVX512N64(SB), NOSPLIT, $16-64
	MOVQ out0+0(FP), R11
	MOVQ out1+8(FP), R10
	MOVQ R11, 0(SP)
	MOVQ R10, 8(SP)
	MOVQ a+16(FP), SI
	MOVQ data+24(FP), DI
	MOVQ scales0+32(FP), AX
	MOVQ scales1+40(FP), BX
	ADDQ rowOff0+48(FP), DI
	MOVQ data+24(FP), R15
	ADDQ rowOff1+56(FP), R15
	// SI = &A0[0]; rows at r*8192(SI). Single base — one ADDQ per K step.

"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	// 64 blocks / 2 per iter = 32 iters
	MOVQ $32, R11
q8d8_n64_loop:
"""
    text += _q8_dual8_loop_body() + "\n"
    text += "\tDECQ R11\n\tJNZ  q8d8_n64_loop\n\n"
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def _dual8_k512_chunk(off: int) -> str:
    """16-float dual-B chunk; SI=&A0[k], rows at r*2048(SI). Pair-load A."""
    if off == 0:
        b0, b1 = "(DI)", "(R15)"
    else:
        b0, b1 = f"{off}(DI)", f"{off}(R15)"
    lines = [f"\tVMOVUPS {b0}, Z16", f"\tVMOVUPS {b1}, Z17"]
    for p in range(0, 8, 2):
        o0 = p * _A_ROW_K512 + off
        o1 = (p + 1) * _A_ROW_K512 + off
        am0 = "(SI)" if o0 == 0 else f"{o0}(SI)"
        am1 = f"{o1}(SI)"
        lines.append(f"\tVMOVUPS {am0}, Z18")
        lines.append(f"\tVMOVUPS {am1}, Z19")
        lines.append(f"\tVFMADD231PS Z18, Z16, Z{p}")
        lines.append(f"\tVFMADD231PS Z18, Z17, Z{8+p}")
        lines.append(f"\tVFMADD231PS Z19, Z16, Z{p+1}")
        lines.append(f"\tVFMADD231PS Z19, Z17, Z{8+p+1}")
    return "\n".join(lines)


def _dual8_k512_loop_body() -> str:
    # v1: half0 all rows then half1 (matches triple v1). A-pair interleave was neutral.
    return "\n".join([
        "\tPREFETCHT0 512(DI)",
        "\tPREFETCHT0 512(R15)",
        "\tPREFETCHT0 512(SI)",
        f"\tPREFETCHT0 {512 + _A_ROW_K512}(SI)",
        f"\tPREFETCHT0 {512 + 2 * _A_ROW_K512}(SI)",
        f"\tPREFETCHT0 {512 + 4 * _A_ROW_K512}(SI)",
        _dual8_k512_chunk(0),
        _dual8_k512_chunk(64),
        "\tADDQ $128, DI",
        "\tADDQ $128, R15",
        "\tADDQ $128, SI",
    ])


def gen_multidot8_dual_k512() -> str:
    """8 A × 2 B one-pass, K=512 float (encoder dual panel)."""
    dual_items = (
        [{"z": i, "mem": f"{i*4}(R11)"} for i in range(4)]
        + [{"z": 8 + i, "mem": f"{16+i*4}(R11)"} for i in range(4)]
        + [{"z": 4 + i, "mem": f"{i*4}(BX)"} for i in range(4)]
        + [{"z": 12 + i, "mem": f"{16+i*4}(BX)"} for i in range(4)]
    )
    stores = [hsum_batch_ops(dual_items, "mem")]

    text = """// func multiDot8DualBAVX512K512(out0, out1 *[8]float32, a, b0, b1 *float32)
// 8 A × 2 B, K=512, one B pass. Single SI A-base (r*2048).
// Frame: out0+0 out1+8 a+16 b0+24 b1+32 → 40
TEXT ·multiDot8DualBAVX512K512(SB), NOSPLIT, $0-40
	MOVQ out0+0(FP), R11
	MOVQ out1+8(FP), BX
	MOVQ a+16(FP), SI
	MOVQ b0+24(FP), DI
	MOVQ b1+32(FP), R15
"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $16, R8
dual8_512_loop:
"""
    text += _dual8_k512_loop_body() + "\n"
    text += "\tDECQ R8\n\tJNZ  dual8_512_loop\n\n"
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


# SenseVoice entry: feats_dim=560. Row stride 560*4=2240. 560/16=35 chunks.
_A_ROW_K560 = 2240


def gen_multidot4_dual_k560() -> str:
    """4 A × 2 B, K=560 float (entry linear dual panel)."""
    # out layout dual4: [0:4]=B0, [4:8]=B1
    items = (
        [{"z": i, "mem": f"{i*4}(R11)"} for i in range(4)]
        + [{"z": 4 + i, "mem": f"{(4+i)*4}(R11)"} for i in range(4)]
    )
    stores = [hsum_batch_ops(items, "mem")]
    text = """// func multiDot4DualBAVX512K560(out *[8]float32, a, b0, b1 *float32)
// 4 A × 2 B, K=560, 16-wide. Frame: out+0 a+8 b0+16 b1+24 → 32
TEXT ·multiDot4DualBAVX512K560(SB), NOSPLIT, $0-32
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
"""
    for i in range(8):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $35, R8
d4_560_loop:
	PREFETCHT0 128(DI)
	PREFETCHT0 128(R15)
	PREFETCHT0 128(SI)
	VMOVUPS (DI), Z16
	VMOVUPS (R15), Z17
"""
    for p in range(0, 4, 2):
        o0 = p * _A_ROW_K560
        o1 = (p + 1) * _A_ROW_K560
        am0 = "(SI)" if o0 == 0 else f"{o0}(SI)"
        text += f"\tVMOVUPS {am0}, Z18\n"
        text += f"\tVMOVUPS {o1}(SI), Z19\n"
        text += f"\tVFMADD231PS Z18, Z16, Z{p}\n"
        text += f"\tVFMADD231PS Z18, Z17, Z{4+p}\n"
        text += f"\tVFMADD231PS Z19, Z16, Z{p+1}\n"
        text += f"\tVFMADD231PS Z19, Z17, Z{4+p+1}\n"
    text += """	ADDQ $64, DI
	ADDQ $64, R15
	ADDQ $64, SI
	DECQ R8
	JNZ  d4_560_loop

"""
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_dual_k560() -> str:
    """8 A × 2 B one-pass, K=560 float (entry dual panel)."""
    dual_items = (
        [{"z": i, "mem": f"{i*4}(R11)"} for i in range(4)]
        + [{"z": 8 + i, "mem": f"{16+i*4}(R11)"} for i in range(4)]
        + [{"z": 4 + i, "mem": f"{i*4}(BX)"} for i in range(4)]
        + [{"z": 12 + i, "mem": f"{16+i*4}(BX)"} for i in range(4)]
    )
    stores = [hsum_batch_ops(dual_items, "mem")]
    text = """// func multiDot8DualBAVX512K560(out0, out1 *[8]float32, a, b0, b1 *float32)
// 8 A × 2 B, K=560, one B pass. SI A-base stride 2240.
// Frame: out0+0 out1+8 a+16 b0+24 b1+32 → 40
TEXT ·multiDot8DualBAVX512K560(SB), NOSPLIT, $0-40
	MOVQ out0+0(FP), R11
	MOVQ out1+8(FP), BX
	MOVQ a+16(FP), SI
	MOVQ b0+24(FP), DI
	MOVQ b1+32(FP), R15
"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $35, R8
dual8_560_loop:
	PREFETCHT0 128(DI)
	PREFETCHT0 128(R15)
	PREFETCHT0 128(SI)
	VMOVUPS (DI), Z16
	VMOVUPS (R15), Z17
"""
    # rows 0-3 → Z0-3 (B0) Z8-11 (B1); rows 4-7 → Z4-7 (B0) Z12-15 (B1)
    # Match dual_items / dual4 layout used by storeDual4
    for p in range(0, 4, 2):
        o0 = p * _A_ROW_K560
        o1 = (p + 1) * _A_ROW_K560
        am0 = "(SI)" if o0 == 0 else f"{o0}(SI)"
        text += f"\tVMOVUPS {am0}, Z18\n"
        text += f"\tVMOVUPS {o1}(SI), Z19\n"
        text += f"\tVFMADD231PS Z18, Z16, Z{p}\n"
        text += f"\tVFMADD231PS Z18, Z17, Z{8+p}\n"
        text += f"\tVFMADD231PS Z19, Z16, Z{p+1}\n"
        text += f"\tVFMADD231PS Z19, Z17, Z{8+p+1}\n"
    for p in range(0, 4, 2):
        o0 = (4 + p) * _A_ROW_K560
        o1 = (5 + p) * _A_ROW_K560
        text += f"\tVMOVUPS {o0}(SI), Z18\n"
        text += f"\tVMOVUPS {o1}(SI), Z19\n"
        text += f"\tVFMADD231PS Z18, Z16, Z{4+p}\n"
        text += f"\tVFMADD231PS Z18, Z17, Z{12+p}\n"
        text += f"\tVFMADD231PS Z19, Z16, Z{4+p+1}\n"
        text += f"\tVFMADD231PS Z19, Z17, Z{12+p+1}\n"
    text += """	ADDQ $64, DI
	ADDQ $64, R15
	ADDQ $64, SI
	DECQ R8
	JNZ  dual8_560_loop

"""
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_dual_plain_k512(n_cols: int) -> str:
    """Fused 8A×2B + bias store for encoder N=512/1536 dual remainder."""
    row_bytes = n_cols * 4
    if n_cols == 512:
        base_calc = "\tSHLQ $9, AX // m*512\n"
        label = "N512"
        loop = "d8_pl512_loop"
    elif n_cols == 1536:
        base_calc = "\tIMULQ $1536, AX\n"
        label = "N1536"
        loop = "d8_pl1536_loop"
    else:
        raise ValueError(n_cols)

    # Match working triple frames: after m/n (8-byte slots), float32s pack at +0/+4.
    # Frame: out+0 a+8 b0+16 b1+24 m+32 n+40 bn0+48 bn1+52 → 56
    items = []
    for r in range(4):
        off = r * row_bytes
        items.append({"z": r, "mem": f"{off}(R11)", "bn": "bn0+48(FP)"})
        items.append({"z": 8 + r, "mem": f"{off+4}(R11)", "bn": "bn1+52(FP)"})
    for r in range(4):
        off = (4 + r) * row_bytes
        items.append({"z": 4 + r, "mem": f"{off}(R11)", "bn": "bn0+48(FP)"})
        items.append({"z": 12 + r, "mem": f"{off+4}(R11)", "bn": "bn1+52(FP)"})
    stores = ["\tMOVQ 0(SP), R11", hsum_batch_ops(items, "bias")]

    text = f"""// func multiDot8DualPlainAVX512K512{label}(out *float32, a, b0, b1 *float32, m, n int, bn0, bn1 float32)
// Encoder dual remainder: 8A×2B K=512 + bias, N={n_cols}.
// Frame: out+0 a+8 b0+16 b1+24 m+32 n+40 bn0+48 bn1+52 → 56
// Stack $8: out base.
TEXT ·multiDot8DualPlainAVX512K512{label}(SB), NOSPLIT, $8-56
	MOVQ out+0(FP), R11
	MOVQ m+32(FP), AX
	MOVQ n+40(FP), BX
{base_calc}	ADDQ BX, AX
	SHLQ $2, AX
	ADDQ AX, R11
	MOVQ R11, 0(SP)

	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += f"""
	MOVQ $16, R8
{loop}:
"""
    text += _dual8_k512_loop_body() + "\n"
    text += f"\tDECQ R8\n\tJNZ  {loop}\n\n"
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_dual_argmax_k512() -> str:
    """CTC dual remainder: 8A×2B K=512 + bias + in-place argmax.

    Same two-phase post-FMA as triple argmax (stack hsums then UCOMISS on X0/X1).
    Dual Z layout matches multiDot8DualB:
      rows0-3 B0=Z0-3 B1=Z8-11; rows4-7 B0=Z4-7 B1=Z12-15.
    """
    # Frame: bestV+0 bestI+8 a+16 b0+24 b1+32 n+40 bn0+48 bn1+52 → 56
    # Stack $64: 16 float32 hsums  layout stack[r*2+c]
    z_for = []
    for r in range(8):
        if r < 4:
            z_for.append((r, 8 + r))
        else:
            rr = r - 4
            z_for.append((4 + rr, 12 + rr))

    p1_items = []
    for r, (z0, z1) in enumerate(z_for):
        p1_items.append({"z": z0, "mem": f"{(r * 2 + 0) * 4}(SP)", "bn": "bn0+48(FP)"})
        p1_items.append({"z": z1, "mem": f"{(r * 2 + 1) * 4}(SP)", "bn": "bn1+52(FP)"})
    phase1 = hsum_batch_ops(p1_items, "bias")

    phase2 = []
    label_i = 0
    for r in range(8):
        vo, io = r * 4, r * 8
        for c in range(2):
            lab = f"damx_{label_i}"
            label_i += 1
            soff = (r * 2 + c) * 4
            phase2.append(f"\tVMOVSS {soff}(SP), X0")
            phase2.append(f"\tVMOVSS {vo}(R11), X1")
            phase2.append("\tUCOMISS X1, X0")
            phase2.append(f"\tJLS  {lab}")
            phase2.append(f"\tVMOVSS X0, {vo}(R11)")
            if c == 0:
                phase2.append(f"\tMOVQ SI, {io}(BX)")
            else:
                phase2.append(f"\tLEAQ {c}(SI), DX")
                phase2.append(f"\tMOVQ DX, {io}(BX)")
            phase2.append(f"{lab}:")

    text = """// func multiDot8DualArgmaxAVX512K512(bestV *float32, bestI *int, a, b0, b1 *float32, n int, bn0, bn1 float32)
// CTC dual remainder: 8A×2B K=512 + bias + argmax update of bestV/bestI (8 rows).
// Frame: bestV+0 bestI+8 a+16 b0+24 b1+32 n+40 bn0+48 bn1+52 → 56
// Stack $64: 16 hsum+bias scalars before UCOMISS (X0-X15 only).
TEXT ·multiDot8DualArgmaxAVX512K512(SB), NOSPLIT, $64-56
	MOVQ a+16(FP), SI
	MOVQ b0+24(FP), DI
	MOVQ b1+32(FP), R15
"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $16, R8
d8_amx_loop:
"""
    text += _dual8_k512_loop_body() + "\n"
    text += "\tDECQ R8\n\tJNZ  d8_amx_loop\n\n"
    text += phase1 + "\n"
    text += """
	MOVQ bestV+0(FP), R11
	MOVQ bestI+8(FP), BX
	MOVQ n+40(FP), SI

"""
    text += "\n".join(phase2) + "\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot8_dual_relu_k512_n2048() -> str:
    """FFN up dual remainder: 8A×2B + bias + ReLU, N=2048."""
    items = []
    for r in range(4):
        off = r * 8192
        items.append({"z": r, "mem": f"{off}(R11)", "bn": "bn0+48(FP)", "zero": "X31"})
        items.append({"z": 8 + r, "mem": f"{off+4}(R11)", "bn": "bn1+52(FP)", "zero": "X31"})
    for r in range(4):
        off = (4 + r) * 8192
        items.append({"z": 4 + r, "mem": f"{off}(R11)", "bn": "bn0+48(FP)", "zero": "X31"})
        items.append({"z": 12 + r, "mem": f"{off+4}(R11)", "bn": "bn1+52(FP)", "zero": "X31"})
    stores = [
        "\tMOVQ 0(SP), R11",
        "\tVXORPS X31, X31, X31",
        hsum_batch_ops(items, "relu"),
    ]
    text = """// func multiDot8DualReLUAVX512K512N2048(out *float32, a, b0, b1 *float32, m, n int, bn0, bn1 float32)
// FFN up dual remainder: 8A×2B K=512 + bias + ReLU, N=2048.
// Frame: out+0 a+8 b0+16 b1+24 m+32 n+40 bn0+48 bn1+52 → 56
TEXT ·multiDot8DualReLUAVX512K512N2048(SB), NOSPLIT, $8-56
	MOVQ out+0(FP), R11
	MOVQ m+32(FP), AX
	MOVQ n+40(FP), BX
	SHLQ $11, AX // m*2048
	ADDQ BX, AX
	SHLQ $2, AX
	ADDQ AX, R11
	MOVQ R11, 0(SP)

	MOVQ a+8(FP), SI
	MOVQ b0+16(FP), DI
	MOVQ b1+24(FP), R15
"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	MOVQ $16, R8
d8_relu_loop:
"""
    text += _dual8_k512_loop_body() + "\n"
    text += "\tDECQ R8\n\tJNZ  d8_relu_loop\n\n"
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8_dual8_accum_n64() -> str:
    """Like dual8 N64 but fuse residual+bias store for N=512 (FFN down).
    out[m*512+n+r*512] += row·B0 + bn0 (and +1 for B1).
    Accums same dual4 layout as dual8.
    """
    # Fused store: row stride 512*4=2048. Bias from FP (batch hsum uses Y24-27).
    acc_items = []
    for r in range(4):
        off = r * 2048
        acc_items.append({"z": r, "mem": f"{off}(R11)", "bn": "bn0+72(FP)"})
        acc_items.append({"z": 4 + r, "mem": f"{off+4}(R11)", "bn": "bn1+76(FP)"})
    for r in range(4):
        off = (4 + r) * 2048
        acc_items.append({"z": 8 + r, "mem": f"{off}(R11)", "bn": "bn0+72(FP)"})
        acc_items.append({"z": 12 + r, "mem": f"{off+4}(R11)", "bn": "bn1+76(FP)"})
    stores = [
        "\tMOVQ 0(SP), R11",
        hsum_batch_ops(acc_items, "accum"),
    ]

    text = """// func q8DualMultiDot8AccumAVX512N64(out *float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1 int, m, n int, bn0, bn1 float32)
// FFN down fused: 8A×2B one-pass + residual/bias store for N=512.
// out points to &out[0]; writes rows m..m+7, cols n,n+1.
// Frame: out+0 a+8 data+16 s0+24 s1+32 off0+40 off1+48 m+56 n+64 bn0+72 bn1+76 → 80
// Stack $16: save out base address (m*N+n).
TEXT ·q8DualMultiDot8AccumAVX512N64(SB), NOSPLIT, $16-80
	MOVQ out+0(FP), R11
	MOVQ m+56(FP), AX
	MOVQ n+64(FP), BX
	// base = (m*512 + n) * 4
	SHLQ $9, AX // m*512
	ADDQ BX, AX
	SHLQ $2, AX
	ADDQ AX, R11
	MOVQ R11, 0(SP)
	// Pull residual destination into cache before FMA (accum path).
	PREFETCHT0 (R11)
	PREFETCHT0 2048(R11)
	PREFETCHT0 4096(R11)
	PREFETCHT0 6144(R11)
	PREFETCHT0 8192(R11)
	PREFETCHT0 10240(R11)
	PREFETCHT0 12288(R11)
	PREFETCHT0 14336(R11)

	MOVQ a+8(FP), SI
	MOVQ data+16(FP), DI
	MOVQ scales0+24(FP), AX
	MOVQ scales1+32(FP), BX
	ADDQ rowOff0+40(FP), DI
	MOVQ data+16(FP), R15
	ADDQ rowOff1+48(FP), R15
	// SI = &A0[0]; rows at r*8192(SI).

"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += """
	// 64 blocks / 2 per iter = 32 iters
	MOVQ $32, R11
q8d8acc_loop:
"""
    text += _q8_dual8_loop_body() + "\n"
    text += "\tDECQ R11\n\tJNZ  q8d8acc_loop\n\n"
    text += "\n".join(stores) + "\n\tVZEROUPPER\n\tRET\n"
    return text


def gen_quantize_panel8_q8u_k2048() -> str:
    """quantizePanel8Q8UAVX512: 8 A rows K=2048 → u8 (0..127) + f32 scales.

    FFN-down A is ReLU output (A≥0). Layout (block-major for VNNI):
      q[b*256 + r*32 : +32] int8
      s[b*8 + r] float32 scale (amax/127)
    """
    # Frame: q+0 s+8 a+16 → 24
    text = """// func quantizePanel8Q8UAVX512(q *int8, s *float32, a *float32)
// 8×2048 ReLU A → u8 Q8 blocks (scale = amax/127).
TEXT ·quantizePanel8Q8UAVX512(SB), NOSPLIT, $0-24
	MOVQ q+0(FP), DI
	MOVQ s+8(FP), R8
	MOVQ a+16(FP), SI
	MOVQ $8, R9
	VXORPS X15, X15, X15
	// X14 = 127.0
	MOVSS $0x42fe0000, X14 // 127.0f bit pattern via immediate load workaround
"""
    # Go asm may not like MOVSS immediate float. Use VBROADCASTSS from const or compute.
    # Better: load 127 from stack or use integer 127 and convert.
    text = """// func quantizePanel8Q8UAVX512(q *int8, s *float32, a *float32)
// 8×2048 non-neg A → u8 packs + f32 scales (scale=amax/127).
// Frame: q+0 s+8 a+16 → 24
TEXT ·quantizePanel8Q8UAVX512(SB), NOSPLIT, $16-24
	MOVQ q+0(FP), DI
	MOVQ s+8(FP), R8
	MOVQ a+16(FP), SI
	// 127.0f on stack
	MOVL $0x42fe0000, AX
	MOVL AX, 0(SP)
	VBROADCASTSS 0(SP), Z14
	// tiny epsilon
	MOVL $0x34000000, AX // ~1.19e-7
	MOVL AX, 4(SP)
	VBROADCASTSS 4(SP), Z15

	MOVQ $8, R9
q8u_row_loop:
	MOVQ $64, R10
	MOVQ SI, R11
	MOVQ DI, R12
	MOVQ R8, R13
q8u_blk_loop:
	// max of 32 floats
	VMOVUPS (R11), Z0
	VMOVUPS 64(R11), Z1
	VMAXPS Z0, Z1, Z2
	VEXTRACTF32X8 $1, Z2, Y3
	VMAXPS Y2, Y3, Y2
	VEXTRACTF32X4 $1, Y2, X3
	VMAXPS X2, X3, X2
	VSHUFPD $1, X2, X2, X3
	VMAXPS X2, X3, X2
	VMOVSHDUP X2, X3
	VMAXSS X2, X3, X2
	// scale = max/127; inv = 127/max (0 if max tiny)
	VMAXSS X15, X2, X2
	// compare max > eps
	UCOMISS X15, X2
	JBE  q8u_zero_blk
	VDIVSS X14, X2, X4 // X4 = max/127 = scale (X14=127, X2=max → need max/127)
"""
    # Wait VDIVSS dividend/divisor order in Go: VDIVSS X3, X2, X1 means X1 = X2/X3?
    # Intel: VDIVSS xmm1, xmm2, xmm3 is xmm1 = xmm2/xmm3
    # Go asm typically: VDIVSS X3, X2, X1 → X1 = X2 / X3
    # From their hsum they use VADDSS X28, X29, X28 for X28 = X28+X29 style...
    # Looking at codebase: VADDSS bn, X28, X28 → X28 = bn + X28, dest last.
    # VDIVSS: if dest last, VDIVSS X14, X2, X4 → X4 = X2/X14 = max/127. Good.
    
    text = """// func quantizePanel8Q8UAVX512(q *int8, s *float32, a *float32)
// 8×2048 non-neg A → u8 packs + f32 scales (scale=amax/127).
// Frame: q+0 s+8 a+16 → 24
TEXT ·quantizePanel8Q8UAVX512(SB), NOSPLIT, $16-24
	MOVQ q+0(FP), DI
	MOVQ s+8(FP), R8
	MOVQ a+16(FP), SI
	MOVL $0x42fe0000, AX // 127.0f
	MOVL AX, 0(SP)
	VBROADCASTSS 0(SP), Z14
	MOVL $0x34000000, AX // ~1.2e-7
	MOVL AX, 4(SP)
	VBROADCASTSS 4(SP), Z15

	MOVQ $8, R9
q8u_row_loop:
	MOVQ $64, R10
	MOVQ SI, R11
	MOVQ DI, R12
	MOVQ R8, R13
q8u_blk_loop:
	VMOVUPS (R11), Z0
	VMOVUPS 64(R11), Z1
	VMAXPS Z0, Z1, Z2
	VEXTRACTF32X8 $1, Z2, Y3
	VMAXPS Y2, Y3, Y2
	VEXTRACTF32X4 $1, Y2, X3
	VMAXPS X2, X3, X2
	VSHUFPD $1, X2, X2, X3
	VMAXPS X2, X3, X2
	VMOVSHDUP X2, X3
	VMAXSS X2, X3, X2
	// X2 = amax; if amax <= eps → zero block
	UCOMISS X15, X2
	JBE  q8u_zero_blk
	// scale = amax/127 → X4; inv = 127/amax → broadcast Z5
	VDIVSS X14, X2, X4
	VDIVSS X2, X14, X5
	VBROADCASTSS X5, Z5
	VMOVSS X4, (R13)
	// quantize 32 floats → 32 int8
	VMULPS Z5, Z0, Z0
	VMULPS Z5, Z1, Z1
	VCVTPS2DQ Z0, Z0
	VCVTPS2DQ Z1, Z1
	// pack int32 → int16 → int8 (signed pack, values 0..127)
	VPACKSSDW Y0, Y0, Y6
	VEXTRACTI32X8 $1, Z0, Y7
	VPACKSSDW Y7, Y7, Y7
	// Y6 low 8 i16 from first 8 i32 of Z0; need full pack carefully
	// Redo pack: VPACKSSDW packs 8+8 int32 → 8 int16 per lane pair
	// Z0 has 16 int32. Split to two YMM.
	VEXTRACTI32X8 $1, Z0, Y3
	VPACKSSDW Y0, Y3, Y0 // 16 int16 in Y0? VPACKSSDW Y1,Y2,Y3: dest = pack(y2.lo,y1.lo) in low, pack(y2.hi,y1.hi) in high — messy
"""
    # Packing is error-prone. Use simpler per-16 approach with VPMOVDB (AVX512)
    # VPMOVDB xmm/m, zmm: pack 16 int32 to 16 int8 with saturation!
    
    # v6: dual-row ILP + block-major q/s.
    # Two A rows (16KB) stay in L1; independent max/mul/pack chains dual-issue on Zen4.
    # q[b*256+r*32]; s[b*8+r]. Constants X13=1/127, X14/Z14=127, X15=eps must not be clobbered.
    def dual_block(a_off: int, q_off: int, s_off: int) -> str:
        am0 = "(R11)" if a_off == 0 else f"{a_off}(R11)"
        am1 = f"{a_off + 64}(R11)"
        bm0 = "(R15)" if a_off == 0 else f"{a_off}(R15)"
        bm1 = f"{a_off + 64}(R15)"
        qm0 = "(R12)" if q_off == 0 else f"{q_off}(R12)"
        qm0b = f"{q_off + 16}(R12)"
        # row1 q is +32 bytes within the same block tile
        qm1 = f"{q_off + 32}(R12)"
        qm1b = f"{q_off + 48}(R12)"
        sm0 = "(R13)" if s_off == 0 else f"{s_off}(R13)"
        sm1 = f"{s_off + 4}(R13)"
        lines = [
            f"\tVMOVUPS {am0}, Z0",
            f"\tVMOVUPS {am1}, Z1",
            f"\tVMOVUPS {bm0}, Z8",
            f"\tVMOVUPS {bm1}, Z9",
            # --- amax row0 → X2 ---
            "\tVMAXPS Z0, Z1, Z2",
            "\tVEXTRACTF32X8 $1, Z2, Y3",
            "\tVMAXPS Y2, Y3, Y2",
            "\tVEXTRACTF32X4 $1, Y2, X3",
            "\tVMAXPS X2, X3, X2",
            "\tVSHUFPD $1, X2, X2, X3",
            "\tVMAXPS X2, X3, X2",
            "\tVMOVSHDUP X2, X3",
            "\tVMAXSS X2, X3, X2",
            # --- amax row1 → X10 ---
            "\tVMAXPS Z8, Z9, Z10",
            "\tVEXTRACTF32X8 $1, Z10, Y11",
            "\tVMAXPS Y10, Y11, Y10",
            "\tVEXTRACTF32X4 $1, Y10, X11",
            "\tVMAXPS X10, X11, X10",
            "\tVSHUFPD $1, X10, X10, X11",
            "\tVMAXPS X10, X11, X10",
            "\tVMOVSHDUP X10, X11",
            "\tVMAXSS X10, X11, X10",
            # --- row0 scale/inv/pack ---
            "\tVMULSS X13, X2, X4",       # scale0
            "\tVMAXSS X15, X2, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            "\tVMOVSS X4, " + sm0,
            "\tVMULPS Z5, Z0, Z0",
            "\tVMULPS Z5, Z1, Z1",
            "\tVCVTPS2DQ Z0, Z0",
            "\tVCVTPS2DQ Z1, Z1",
            "\tVPMOVDB Z0, X6",
            "\tVPMOVDB Z1, X7",
            f"\tVMOVUPS X6, {qm0}",
            f"\tVMOVUPS X7, {qm0b}",
            # --- row1 scale/inv/pack ---
            "\tVMULSS X13, X10, X4",      # scale1 (reuse X4)
            "\tVMAXSS X15, X10, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            "\tVMOVSS X4, " + sm1,
            "\tVMULPS Z5, Z8, Z8",
            "\tVMULPS Z5, Z9, Z9",
            "\tVCVTPS2DQ Z8, Z8",
            "\tVCVTPS2DQ Z9, Z9",
            "\tVPMOVDB Z8, X6",
            "\tVPMOVDB Z9, X7",
            f"\tVMOVUPS X6, {qm1}",
            f"\tVMOVUPS X7, {qm1b}",
        ]
        return "\n".join(lines)

    text = """// func quantizePanel8Q8UAVX512(q *int8, s *float32, a *float32)
// 8×2048 non-neg A → u8 block-major packs + f32 scales (v6 dual-row ILP).
// q layout: block-major [64][8][32]; s: block-major [64][8].
// Frame: q+0 s+8 a+16 → 24
TEXT ·quantizePanel8Q8UAVX512(SB), NOSPLIT, $16-24
	MOVQ q+0(FP), DI
	MOVQ s+8(FP), R8
	MOVQ a+16(FP), SI
	MOVL $0x42fe0000, AX // 127.0f
	MOVL AX, 0(SP)
	VBROADCASTSS 0(SP), Z14
	MOVL $0x34000000, AX // ~1.2e-7f eps
	MOVL AX, 4(SP)
	VMOVSS 4(SP), X15
	MOVL $0x3c010204, AX // 1/127.0f
	MOVL AX, 8(SP)
	VMOVSS 8(SP), X13

	// 4 pairs of rows (0-1, 2-3, 4-5, 6-7)
	MOVQ $4, R9
	MOVQ $0, R14 // pair index 0..3 → row = pair*2
q8u_pair_loop:
	MOVQ $32, R10 // 64/2 blocks
	// R11=&A[r], R15=&A[r+1]; R12=&q[r*32]; R13=&s[r]
	MOVQ R14, AX
	SHLQ $1, AX // r = pair*2
	// a0 = SI + r*8192
	MOVQ AX, BX
	SHLQ $13, BX // r*8192
	LEAQ (SI)(BX*1), R11
	LEAQ 8192(R11), R15
	// q0 = DI + r*32
	MOVQ AX, BX
	SHLQ $5, BX // r*32
	LEAQ (DI)(BX*1), R12
	// s0 = R8 + r*4
	LEAQ (R8)(AX*4), R13
q8u_blk_loop:
	PREFETCHT0 256(R11)
	PREFETCHT0 256(R15)
"""
    text += dual_block(0, 0, 0) + "\n"
    text += dual_block(128, 256, 32) + "\n"
    text += """	ADDQ $256, R11 // A: 64 floats
	ADDQ $256, R15
	ADDQ $512, R12 // q: 2 blocks × 256
	ADDQ $64, R13 // s: 2 blocks × 8 floats
	DECQ R10
	JNZ  q8u_blk_loop
	INCQ R14
	DECQ R9
	JNZ  q8u_pair_loop
	VZEROUPPER
	RET
"""
    return text





def _vnni_hsum8_f32_to_x28(yreg: int) -> str:
    """Horizontal sum of 8 float32 in Y{yreg} → X28. Temps Y28/X29 (post-loop only)."""
    return "\n".join([
        f"\tVEXTRACTF32X4 $1, Y{yreg}, X29",
        f"\tVADDPS X{yreg}, X29, X28",
        "\tVSHUFPD $1, X28, X28, X29",
        "\tVADDPS X28, X29, X28",
        "\tVMOVSHDUP X28, X29",
        "\tVADDSS X28, X29, X28",
    ])


def _vnni_hsum_batch_accum_store(items: list) -> str:
    """Parallel hsum of YMM float accums + residual/bias store.

    items: dicts with y (Y reg), mem (dest), bn (FP bias mem).
    After FMA loop Y0-15 hold accums; temps X16-X19 (partials), X20/X21 (finish).
    """
    lines = []
    for i in range(0, len(items), 4):
        group = items[i : i + 4]
        # Phase1: 8→4 floats into X16..X19
        for j, it in enumerate(group):
            xt = 16 + j
            y = it["y"]
            lines.append(f"\tVEXTRACTF32X4 $1, Y{y}, X{xt}")
            lines.append(f"\tVADDPS X{y}, X{xt}, X{xt}")
        # Phase2: 4→1 + bias + residual store
        for j, it in enumerate(group):
            xt = 16 + j
            lines.append(f"\tVSHUFPD $1, X{xt}, X{xt}, X20")
            lines.append(f"\tVADDPS X{xt}, X20, X{xt}")
            lines.append(f"\tVMOVSHDUP X{xt}, X20")
            lines.append(f"\tVADDSS X{xt}, X20, X{xt}")
            lines.append(f"\tVADDSS {it['bn']}, X{xt}, X{xt}")
            lines.append(f"\tVADDSS {it['mem']}, X{xt}, X{xt}")
            lines.append(f"\tVMOVSS X{xt}, {it['mem']}")
    return "\n".join(lines)


def _vnni_dual4(row0: int) -> str:
    """4-row ILP dual-B against block-major A + block-major scales.

    A layout: q[b*256 + r*32]; SI points at block base (row0 at 0(SI)).
    Scales: s[b*8 + r]; R8=&s[b*8+0], sa at r*4(R8) — 8 scales in one cache line.
    row0: 0 or 4. Accums Y0-Y7 (B0), Y8-Y15 (B1). B in Y16/Y17.
    X28/X29 hold sb0/sb1; product via VMULSS (v10 hoist sb+VMULPS measured slower;
    full vector sa*sb precompute+extract also slower).
    """
    lines = []
    for i in range(4):
        r = row0 + i
        # Contiguous 32B per row within block: stride 32 not 2048.
        lines.append(f"\tVMOVDQU32 {r * 32}(SI), Y{18 + i}")
    if row0 == 0:
        lines.append("\tPREFETCHT0 128(SI)")  # rows 4-7 same block
    for i in range(4):
        lines.append(f"\tVPXORD Y{22 + i}, Y{22 + i}, Y{22 + i}")
        lines.append(f"\tVPDPBUSD Y16, Y{18 + i}, Y{22 + i}")
    for i in range(4):
        r = row0 + i
        lines.append(f"\tVCVTDQ2PS Y{22 + i}, Y{22 + i}")
        lines.append(f"\tVMULSS {r * 4}(R8), X28, X27")
        lines.append("\tVBROADCASTSS X27, Y26")
        lines.append(f"\tVFMADD231PS Y26, Y{22 + i}, Y{r}")
    for i in range(4):
        lines.append(f"\tVPXORD Y{22 + i}, Y{22 + i}, Y{22 + i}")
        lines.append(f"\tVPDPBUSD Y17, Y{18 + i}, Y{22 + i}")
    for i in range(4):
        r = row0 + i
        lines.append(f"\tVCVTDQ2PS Y{22 + i}, Y{22 + i}")
        lines.append(f"\tVMULSS {r * 4}(R8), X29, X27")
        lines.append("\tVBROADCASTSS X27, Y26")
        lines.append(f"\tVFMADD231PS Y26, Y{22 + i}, Y{8 + r}")
    return "\n".join(lines)


def gen_q8u_q8s_dual8_accum_vnni() -> str:
    """FFN down: 8 A_u8 × 2 B_s8 VNNI + residual/bias store N=512.

    v8: block-major A + block-major scales + vector float accums + dual-B.
    Accums Y0-Y7 (B0), Y8-Y15 (B1). B0/B1 in Y16/Y17.
    """
    text = """// func q8uQ8sDual8AccumVNNI(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// FFN down VNNI v8: block-major A_u8 + block-major A scales + YMM VPDPBUSD. N=512 K=2048.
// Frame: out+0 aQ+8 aS+16 data+24 sB0+32 sB1+40 off0+48 off1+56 m+64 n+72 bn0+80 bn1+84 → 88
TEXT ·q8uQ8sDual8AccumVNNI(SB), NOSPLIT, $8-88
	MOVQ out+0(FP), R11
	MOVQ m+64(FP), AX
	MOVQ n+72(FP), BX
	SHLQ $9, AX
	ADDQ BX, AX
	SHLQ $2, AX
	ADDQ AX, R11
	MOVQ R11, 0(SP)

	MOVQ aQ+8(FP), SI
	MOVQ aS+16(FP), R8
	MOVQ data+24(FP), DI
	MOVQ sB0+32(FP), AX
	MOVQ sB1+40(FP), BX
	ADDQ off0+48(FP), DI
	MOVQ data+24(FP), R15
	ADDQ off1+56(FP), R15

"""
    for i in range(16):
        text += f"\tVXORPS Y{i}, Y{i}, Y{i}\n"
    def one_block() -> str:
        return "\n".join([
            "\tPREFETCHT0 102(DI)",
            "\tPREFETCHT0 102(R15)",
            "\tPREFETCHT0 512(SI)",  # next block's 8 rows
            "\tVMOVDQU32 2(DI), Y16",
            "\tVMOVDQU32 2(R15), Y17",
            "\tVMOVSS (AX), X28",
            "\tVMOVSS (BX), X29",
            _vnni_dual4(0),
            _vnni_dual4(4),
            "\tADDQ $34, DI",
            "\tADDQ $34, R15",
            "\tADDQ $4, AX",
            "\tADDQ $4, BX",
            "\tADDQ $256, SI",  # next block-major A tile
            "\tADDQ $32, R8",  # next block's 8 contiguous scales
        ])

    # 32×2 blocks — less loop overhead than 64 single-block iters.
    text += """
	MOVQ $32, R10
q8vnni_loop:
"""
    text += one_block() + "\n"
    text += one_block() + "\n"
    text += """
	DECQ R10
	JNZ  q8vnni_loop

	MOVQ 0(SP), R11
"""
    # Serial hsum+residual (batch form measured neutral/negative vs short epilogue).
    for r in range(8):
        off = r * 2048
        text += _vnni_hsum8_f32_to_x28(r) + "\n"
        text += f"\tVADDSS bn0+80(FP), X28, X28\n"
        text += f"\tVADDSS {off}(R11), X28, X28\n"
        text += f"\tVMOVSS X28, {off}(R11)\n"
        text += _vnni_hsum8_f32_to_x28(8 + r) + "\n"
        text += f"\tVADDSS bn1+84(FP), X28, X28\n"
        text += f"\tVADDSS {off + 4}(R11), X28, X28\n"
        text += f"\tVMOVSS X28, {off + 4}(R11)\n"
    text += """	VZEROUPPER
	RET
"""
    return text


def gen_q8u_q8s_quad8_accum_vnni() -> str:
    """FFN down: 8 A_u8 × 4 B_s8 VNNI with vector float accums.

    Two K-passes of 4 A rows × 4 B (16 YMM vector accums Y0-Y15).
    vs two dual calls: half the A loads (A 4-row panels once each; B streamed twice).
    Accums: row r (0..3 within pass) · B j → Y{j*4+r}.
    """
    text = """// func q8uQ8sQuad8AccumVNNI(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1, sB2, sB3 *float32, off0, off1, off2, off3, m, n int, bn0, bn1, bn2, bn3 float32)
// FFN down VNNI quad v2: 4A×4B vector accums ×2 passes (rows 0-3, 4-7). Frame 128, stack $40.
TEXT ·q8uQ8sQuad8AccumVNNI(SB), NOSPLIT, $40-128
	MOVQ out+0(FP), R11
	MOVQ m+96(FP), AX
	MOVQ n+104(FP), BX
	SHLQ $9, AX
	ADDQ BX, AX
	SHLQ $2, AX
	ADDQ AX, R11
	MOVQ R11, 0(SP)
	// Save B bases for second pass
	MOVQ data+24(FP), DI
	ADDQ off0+64(FP), DI
	MOVQ DI, 8(SP)
	MOVQ data+24(FP), R15
	ADDQ off1+72(FP), R15
	MOVQ R15, 16(SP)
	MOVQ data+24(FP), R14
	ADDQ off2+80(FP), R14
	MOVQ R14, 24(SP)
	MOVQ data+24(FP), R13
	ADDQ off3+88(FP), R13
	MOVQ R13, 32(SP)

	MOVQ aQ+8(FP), SI
	MOVQ aS+16(FP), R8
"""
    # Two passes: row_base 0 and 4
    for pass_i, row_base in enumerate([0, 4]):
        lab = f"q8vnni4_p{pass_i}"
        text += f"""
	// pass rows {row_base}..{row_base+3}
	MOVQ 8(SP), DI
	MOVQ 16(SP), R15
	MOVQ 24(SP), R14
	MOVQ 32(SP), R13
	MOVQ sB0+32(FP), AX
	MOVQ sB1+40(FP), BX
	MOVQ sB2+48(FP), CX
	MOVQ sB3+56(FP), DX
	MOVQ aQ+8(FP), SI
	ADDQ ${row_base * 2048}, SI
	MOVQ aS+16(FP), R8
	ADDQ ${row_base * 256}, R8
"""
        for i in range(16):
            text += f"\tVXORPS Y{i}, Y{i}, Y{i}\n"
        text += f"""
	MOVQ $64, R10
{lab}_loop:
	PREFETCHT0 102(DI)
	PREFETCHT0 102(R15)
	VMOVDQU32 2(DI), Y16
	VMOVDQU32 2(R15), Y17
	VMOVDQU32 2(R14), Y18
	VMOVDQU32 2(R13), Y19
	VMOVSS (AX), X28
	VMOVSS (BX), X29
	VMOVSS (CX), X30
	VMOVSS (DX), X31
"""
        # Load 4 A into Y20-Y23
        for i in range(4):
            text += f"\tVMOVDQU32 {i * 2048}(SI), Y{20 + i}\n"
        # For each B j: VPDPBUSD all 4 A into int temps then scale-fmadd.
        # Broadcast uses Y31 (not Y24-Y27 int temps).
        for j, (yb, xs) in enumerate([(16, 28), (17, 29), (18, 30), (19, 31)]):
            for i in range(4):
                text += f"\tVPXORD Y{24 + i}, Y{24 + i}, Y{24 + i}\n"
                text += f"\tVPDPBUSD Y{yb}, Y{20 + i}, Y{24 + i}\n"
            for i in range(4):
                acc = j * 4 + i  # Y0-Y15
                text += f"\tVCVTDQ2PS Y{24 + i}, Y{24 + i}\n"
                text += f"\tVMULSS {i * 256}(R8), X{xs}, X27\n"
                text += "\tVBROADCASTSS X27, Y31\n"
                text += f"\tVFMADD231PS Y31, Y{24 + i}, Y{acc}\n"
        text += f"""
	ADDQ $34, DI
	ADDQ $34, R15
	ADDQ $34, R14
	ADDQ $34, R13
	ADDQ $4, AX
	ADDQ $4, BX
	ADDQ $4, CX
	ADDQ $4, DX
	ADDQ $32, SI
	ADDQ $4, R8
	DECQ R10
	JNZ  {lab}_loop

	MOVQ 0(SP), R11
"""
        # hsum+store rows row_base..row_base+3, cols n..n+3
        for i in range(4):
            r = row_base + i
            off = r * 2048
            for j, bn in enumerate(["bn0+112(FP)", "bn1+116(FP)", "bn2+120(FP)", "bn3+124(FP)"]):
                y = j * 4 + i
                text += _vnni_hsum8_f32_to_x28(y) + "\n"
                text += f"\tVADDSS {bn}, X28, X28\n"
                text += f"\tVADDSS {off + j * 4}(R11), X28, X28\n"
                text += f"\tVMOVSS X28, {off + j * 4}(R11)\n"

    text += """	VZEROUPPER
	RET
"""
    return text


def gen_dot_q8_scaled() -> str:
    """dotQ8RowScaledAVX512: single A × one Q8 B row, f32 scales, 16-wide."""
    # Frame matches AVX2: a+0 data+8 scales+16 rowOff+24 nBlocks+32 ret+40 → 48
    text = """// func dotQ8RowScaledAVX512(a *float32, data *byte, scales *float32, rowOff, nBlocks int) float32
// Single-row Q8 scaled dot, 16-wide (SenseVoice M remainder / M=1).
// Frame: a+0 data+8 scales+16 rowOff+24 nBlocks+32 ret+40 → 48
TEXT ·dotQ8RowScaledAVX512(SB), NOSPLIT, $0-44
	MOVQ a+0(FP), SI
	MOVQ data+8(FP), DI
	MOVQ scales+16(FP), R8
	ADDQ rowOff+24(FP), DI
	MOVQ nBlocks+32(FP), CX
	VXORPS Z0, Z0, Z0
	TESTQ CX, CX
	JZ   dqs512_dot_done

dqs512_dot_loop:
	PREFETCHT0 68(DI)
	PREFETCHT0 256(SI)
	VBROADCASTSS (R8), Z1
	ADDQ $4, R8
	// half0: bytes 2..17 → 16 floats
	VPMOVSXBD 2(DI), Z2
	VCVTDQ2PS Z2, Z2
	VMULPS Z1, Z2, Z2
	VMOVUPS (SI), Z3
	VFMADD231PS Z3, Z2, Z0
	// half1: bytes 18..33
	VPMOVSXBD 18(DI), Z2
	VCVTDQ2PS Z2, Z2
	VMULPS Z1, Z2, Z2
	VMOVUPS 64(SI), Z3
	VFMADD231PS Z3, Z2, Z0
	ADDQ $34, DI
	ADDQ $128, SI
	DECQ CX
	JNZ  dqs512_dot_loop

dqs512_dot_done:
"""
    # hsum Z0 → X0 scalar (same pattern as hsum_z_to_x28 but into X0)
    text += """\tVEXTRACTF32X8 $1, Z0, Y1
	VADDPS Y0, Y1, Y0
	VEXTRACTF32X4 $1, Y0, X1
	VADDPS X0, X1, X0
	VSHUFPD $1, X0, X0, X1
	VADDPS X0, X1, X0
	VMOVSHDUP X0, X1
	VADDSS X0, X1, X0
	MOVSS X0, ret+40(FP)
	VZEROUPPER
	RET
"""
    return text


def gen_dual_dot2_scaled() -> str:
    """q8DualDot2ScaledAVX512: one A × two Q8 B rows, f32 scales, 16-wide."""
    # Frame matches AVX2: out+0 a+8 data+16 s0+24 s1+32 off0+40 off1+48 nBlocks+56 → 64
    text = """// func q8DualDot2ScaledAVX512(out *[2]float32, a *float32, data *byte, scales0, scales1 *float32, rowOff0, rowOff1, nBlocks int)
// Dual-B Q8 scaled dots sharing A stream (M remainder / M=1 dual).
// Frame: out+0 a+8 data+16 s0+24 s1+32 off0+40 off1+48 nBlocks+56 → 64
TEXT ·q8DualDot2ScaledAVX512(SB), NOSPLIT, $0-64
	MOVQ out+0(FP), R11
	MOVQ a+8(FP), SI
	MOVQ data+16(FP), DI
	MOVQ scales0+24(FP), R8
	MOVQ scales1+32(FP), R9
	MOVQ nBlocks+56(FP), CX
	ADDQ rowOff0+40(FP), DI
	MOVQ data+16(FP), R15
	ADDQ rowOff1+48(FP), R15
	VXORPS Z0, Z0, Z0
	VXORPS Z1, Z1, Z1
	TESTQ CX, CX
	JZ   ddsc512_hsum

ddsc512_loop:
	PREFETCHT0 68(DI)
	PREFETCHT0 68(R15)
	PREFETCHT0 256(SI)
	VBROADCASTSS (R8), Z30
	VBROADCASTSS (R9), Z31
	ADDQ $4, R8
	ADDQ $4, R9
	// half0
	VPMOVSXBD 2(DI), Z2
	VPMOVSXBD 2(R15), Z3
	VCVTDQ2PS Z2, Z2
	VCVTDQ2PS Z3, Z3
	VMULPS Z30, Z2, Z2
	VMULPS Z31, Z3, Z3
	VMOVUPS (SI), Z4
	VFMADD231PS Z4, Z2, Z0
	VFMADD231PS Z4, Z3, Z1
	// half1
	VPMOVSXBD 18(DI), Z2
	VPMOVSXBD 18(R15), Z3
	VCVTDQ2PS Z2, Z2
	VCVTDQ2PS Z3, Z3
	VMULPS Z30, Z2, Z2
	VMULPS Z31, Z3, Z3
	VMOVUPS 64(SI), Z4
	VFMADD231PS Z4, Z2, Z0
	VFMADD231PS Z4, Z3, Z1
	ADDQ $34, DI
	ADDQ $34, R15
	ADDQ $128, SI
	DECQ CX
	JNZ  ddsc512_loop

ddsc512_hsum:
"""
    # hsum Z0 → (R11), Z1 → 4(R11). Temps Y2/X2/X3.
    text += """\tVEXTRACTF32X8 $1, Z0, Y2
	VADDPS Y0, Y2, Y0
	VEXTRACTF32X4 $1, Y0, X2
	VADDPS X0, X2, X0
	VSHUFPD $1, X0, X0, X2
	VADDPS X0, X2, X0
	VMOVSHDUP X0, X2
	VADDSS X0, X2, X0
	MOVSS X0, (R11)

	VEXTRACTF32X8 $1, Z1, Y2
	VADDPS Y1, Y2, Y1
	VEXTRACTF32X4 $1, Y1, X2
	VADDPS X1, X2, X1
	VSHUFPD $1, X1, X1, X2
	VADDPS X1, X2, X1
	VMOVSHDUP X1, X2
	VADDSS X1, X2, X1
	MOVSS X1, 4(R11)

	VZEROUPPER
	RET
"""
    return text


def gen_wsum8_dual_128() -> str:
    """wsumBatched8Add128Dual AVX-512: 16-wide over dim=128."""
    # o0..o7, va, vb, wa, wb pointers
    # Frame matches AVX2: many pointer args
    # From fmadd: wsumBatched8Add128DualAVX2(o0..o7, va, vb, wa, wb)
    # Count: 8 o + 2 v + 2 w = 12 pointers = 96 bytes
    text = """// func wsumBatched8Add128DualAVX512(o0,o1,o2,o3,o4,o5,o6,o7, va, vb, wa, wb *float32)
// out_t += wa[t]*va + wb[t]*vb for t=0..7, dim=128, AVX-512 16-wide.
// Frame: o0+0 ... o7+56 va+64 vb+72 wa+80 wb+88 → 96
TEXT ·wsumBatched8Add128DualAVX512(SB), NOSPLIT, $0-96
	MOVQ o0+0(FP), R8
	MOVQ o1+8(FP), R9
	MOVQ o2+16(FP), R10
	MOVQ o3+24(FP), R11
	MOVQ o4+32(FP), R12
	MOVQ o5+40(FP), R13
	MOVQ o6+48(FP), R14
	MOVQ o7+56(FP), R15
	MOVQ va+64(FP), SI
	MOVQ vb+72(FP), DI
	MOVQ wa+80(FP), AX
	MOVQ wb+88(FP), BX

	// Broadcast 8 wa and 8 wb into Z registers — load scalar and broadcast
"""
    # Z16-Z23 = wa[0..7], Z24-Z31 = wb[0..7] — only 32 Z regs, Z0-Z7 for o loads
    # Better: process one 16-float chunk:
    # load va, vb into Z0, Z1
    # for each out: load o, FMA wa*va, FMA wb*vb, store
    # broadcast weights once outside loop into Z16-Z23 and Z24-Z31
    for i in range(8):
        text += f"\tMOVSS {i*4}(AX), X{16+i}\n"
        text += f"\tVBROADCASTSS X{16+i}, Z{16+i}\n"
    for i in range(8):
        text += f"\tMOVSS {i*4}(BX), X{24+i}\n"
        text += f"\tVBROADCASTSS X{24+i}, Z{24+i}\n"
    # Wait - X16-X31 might not work with MOVSS. Use VMOVSS and temps in X0
    # Redo with X0 as temp for broadcast
    text = """// func wsumBatched8Add128DualAVX512(o0,o1,o2,o3,o4,o5,o6,o7, va, vb, wa, wb *float32)
// out_t += wa[t]*va + wb[t]*vb, dim=128, 16-wide.
// Frame: o0+0..o7+56 va+64 vb+72 wa+80 wb+88 → 96
TEXT ·wsumBatched8Add128DualAVX512(SB), NOSPLIT, $0-96
	MOVQ o0+0(FP), R8
	MOVQ o1+8(FP), R9
	MOVQ o2+16(FP), R10
	MOVQ o3+24(FP), R11
	MOVQ o4+32(FP), R12
	MOVQ o5+40(FP), R13
	MOVQ o6+48(FP), R14
	MOVQ o7+56(FP), R15
	MOVQ va+64(FP), SI
	MOVQ vb+72(FP), DI
	MOVQ wa+80(FP), AX
	MOVQ wb+88(FP), BX
"""
    for i in range(8):
        text += f"\tVMOVSS {i*4}(AX), X0\n\tVBROADCASTSS X0, Z{16+i}\n"
    for i in range(8):
        text += f"\tVMOVSS {i*4}(BX), X0\n\tVBROADCASTSS X0, Z{24+i}\n"
    text += """
	MOVQ $8, CX
wsum8_loop:
	VMOVUPS (SI), Z0
	VMOVUPS (DI), Z1
"""
    o_regs = ["R8", "R9", "R10", "R11", "R12", "R13", "R14", "R15"]
    for i, reg in enumerate(o_regs):
        text += f"\tVMOVUPS ({reg}), Z2\n"
        text += f"\tVFMADD231PS Z0, Z{16+i}, Z2\n"
        text += f"\tVFMADD231PS Z1, Z{24+i}, Z2\n"
        text += f"\tVMOVUPS Z2, ({reg})\n"
    text += """	ADDQ $64, SI
	ADDQ $64, DI
	ADDQ $64, R8
	ADDQ $64, R9
	ADDQ $64, R10
	ADDQ $64, R11
	ADDQ $64, R12
	ADDQ $64, R13
	ADDQ $64, R14
	ADDQ $64, R15
	DECQ CX
	JNZ  wsum8_loop
	VZEROUPPER
	RET
"""
    return text


# --- K=512 signed-A VNNI kernels (SenseVoice encoder/CTC) -------------------
#
# Same panel geometry as the K=2048 FFN-down kernels, but the activations are
# signed (LayerNorm output), so A is quantized as unsigned with a +128 bias:
#   A_u = round(A / scale) + 128   (scale = amax/127 per 32-wide block)
# and each VPDPBUSD accumulator is compensated per block:
#   acc_true = VPDPBUSD(B_s8, A_u8) - VPDPBUSD(B_s8, 0x80)
# The per-B compensation vector (Y26/Y24) is computed once per block and
# amortized over the 8 A rows (Gemma-style runtime compensation; a precomputed
# per-block sum(B) table would double the streamed B footprint).

def gen_quantize_panel8_q8s_k512() -> str:
    """quantizePanel8Q8SK512AVX512: 8 A rows K=512 → +128-biased u8 + f32 scales.

    Signed input: per-block amax over |a|; q = round(a*127/amax)+128 ∈ [1,255].
    Layout (block-major for VNNI, 16 blocks):
      q[b*256 + r*32 : +32] byte
      s[b*8 + r] float32 scale (amax/127)
    Constants: Z12=abs mask, Z14=127.0, Z6=128.0, X13=1/127, X15=eps.
    """

    def amax_chain(src: str, tmpy: str, tmpx: str, dst: str) -> list:
        # horizontal max of 16 |floats| in {src} (Z) → X{dst}; tmpy=Y tmp, tmpx=X tmp
        return [
            f"\tVEXTRACTF32X8 $1, {src}, {tmpy}",
            f"\tVMAXPS Y{src[1:]}, {tmpy}, Y{src[1:]}",
            f"\tVEXTRACTF32X4 $1, Y{src[1:]}, X{tmpx}",
            f"\tVMAXPS X{src[1:]}, X{tmpx}, X{src[1:]}",
            f"\tVSHUFPD $1, X{src[1:]}, X{src[1:]}, X{tmpx}",
            f"\tVMAXPS X{src[1:]}, X{tmpx}, X{src[1:]}",
            f"\tVMOVSHDUP X{src[1:]}, X{tmpx}",
            f"\tVMAXSS X{src[1:]}, X{tmpx}, X{dst}",
        ]

    def dual_block(a_off: int, q_off: int, s_off: int) -> str:
        am0 = "(R11)" if a_off == 0 else f"{a_off}(R11)"
        am1 = f"{a_off + 64}(R11)"
        bm0 = "(R15)" if a_off == 0 else f"{a_off}(R15)"
        bm1 = f"{a_off + 64}(R15)"
        qm0 = "(R12)" if q_off == 0 else f"{q_off}(R12)"
        qm0b = f"{q_off + 16}(R12)"
        qm1 = f"{q_off + 32}(R12)"
        qm1b = f"{q_off + 48}(R12)"
        sm0 = "(R13)" if s_off == 0 else f"{s_off}(R13)"
        sm1 = f"{s_off + 4}(R13)"
        lines = [
            f"\tVMOVUPS {am0}, Z0",
            f"\tVMOVUPS {am1}, Z1",
            f"\tVMOVUPS {bm0}, Z8",
            f"\tVMOVUPS {bm1}, Z9",
            # |a| copies for amax (originals kept for the quantize multiply)
            "\tVANDPS Z12, Z0, Z2",
            "\tVANDPS Z12, Z1, Z3",
            "\tVANDPS Z12, Z8, Z10",
            "\tVANDPS Z12, Z9, Z11",
            "\tVMAXPS Z2, Z3, Z2",
            "\tVMAXPS Z10, Z11, Z10",
        ]
        lines += amax_chain("Z2", "Y3", "3", "2")    # amax0 → X2 (Z2 dead)
        lines += amax_chain("Z10", "Y11", "11", "10")  # amax1 → X10 (Z10 dead)
        lines += [
            # row0: scale, inv, quantize, +128 bias, pack
            "\tVMULSS X13, X2, X4",
            "\tVMAXSS X15, X2, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            f"\tVMOVSS X4, {sm0}",
            "\tVMULPS Z5, Z0, Z0",
            "\tVMULPS Z5, Z1, Z1",
            "\tVADDPS Z6, Z0, Z0",
            "\tVADDPS Z6, Z1, Z1",
            "\tVCVTPS2DQ Z0, Z0",
            "\tVCVTPS2DQ Z1, Z1",
            "\tVPMOVDB Z0, X4",
            "\tVPMOVDB Z1, X7",
            f"\tVMOVUPS X4, {qm0}",
            f"\tVMOVUPS X7, {qm0b}",
            # row1
            "\tVMULSS X13, X10, X4",
            "\tVMAXSS X15, X10, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            f"\tVMOVSS X4, {sm1}",
            "\tVMULPS Z5, Z8, Z8",
            "\tVMULPS Z5, Z9, Z9",
            "\tVADDPS Z6, Z8, Z8",
            "\tVADDPS Z6, Z9, Z9",
            "\tVCVTPS2DQ Z8, Z8",
            "\tVCVTPS2DQ Z9, Z9",
            "\tVPMOVDB Z8, X4",
            "\tVPMOVDB Z9, X7",
            f"\tVMOVUPS X4, {qm1}",
            f"\tVMOVUPS X7, {qm1b}",
        ]
        return "\n".join(lines)

    text = """// func quantizePanel8Q8SK512AVX512(q *int8, s *float32, a *float32)
// 8×512 signed A → +128-biased u8 block-major packs + f32 scales (amax/127).
// q layout: block-major [16][8][32]; s: block-major [16][8].
// Frame: q+0 s+8 a+16 → 24
TEXT ·quantizePanel8Q8SK512AVX512(SB), NOSPLIT, $32-24
\tMOVQ q+0(FP), DI
\tMOVQ s+8(FP), R8
\tMOVQ a+16(FP), SI
\tMOVL $0x42fe0000, AX // 127.0f
\tMOVL AX, 0(SP)
\tVBROADCASTSS 0(SP), Z14
\tMOVL $0x34000000, AX // ~1.2e-7f eps
\tMOVL AX, 4(SP)
\tVMOVSS 4(SP), X15
\tMOVL $0x3c010204, AX // 1/127.0f
\tMOVL AX, 8(SP)
\tVMOVSS 8(SP), X13
\tMOVL $0x7fffffff, AX // abs mask
\tMOVL AX, 12(SP)
\tVBROADCASTSS 12(SP), Z12
\tMOVL $0x43000000, AX // 128.0f bias
\tMOVL AX, 16(SP)
\tVBROADCASTSS 16(SP), Z6

\tMOVQ $4, R9
\tMOVQ $0, R14 // pair index 0..3 → row = pair*2
q8s_pair_loop:
\tMOVQ $8, R10 // 16/2 blocks
\tMOVQ R14, AX
\tSHLQ $1, AX // r = pair*2
\tMOVQ AX, BX
\tSHLQ $11, BX // r*2048 (512 floats)
\tLEAQ (SI)(BX*1), R11
\tLEAQ 2048(R11), R15
\tMOVQ AX, BX
\tSHLQ $5, BX // r*32
\tLEAQ (DI)(BX*1), R12
\tLEAQ (R8)(AX*4), R13
q8s_blk_loop:
\tPREFETCHT0 256(R11)
\tPREFETCHT0 256(R15)
"""
    text += dual_block(0, 0, 0) + "\n"
    text += dual_block(128, 256, 32) + "\n"
    text += """\tADDQ $256, R11 // A: 64 floats
\tADDQ $256, R15
\tADDQ $512, R12 // q: 2 blocks × 256
\tADDQ $64, R13 // s: 2 blocks × 8 floats
\tDECQ R10
\tJNZ  q8s_blk_loop
\tINCQ R14
\tDECQ R9
\tJNZ  q8s_pair_loop
\tVZEROUPPER
\tRET
"""
    return text


def _k512_vnni_dual8_loop(label: str) -> str:
    """Shared 16-block K=512 compensated VNNI loop: 8 A × 2 B, vector f32 accums.

    Registers: Y0-7 B0 accums, Y8-15 B1 accums, Y16/17 B0/B1, Y18-21 A rows,
    Y22 int temp, Y23 broadcast temp, Y25 0x80 const, Y26/24 comp0/comp1,
    X27 mul temp, X28/29 sB0/sB1. GP: SI=aQ R8=aS DI=B0 R15=B1 AX=sB0 BX=sB1
    R10=counter. Per block: 2 extra VPDPBUSD (comp) + 16 VPSUBD.
    """
    def block() -> str:
        lines = [
            "\tPREFETCHT0 102(DI)",
            "\tPREFETCHT0 102(R15)",
            "\tPREFETCHT0 512(SI)",
            "\tVMOVDQU32 2(DI), Y16",
            "\tVMOVDQU32 2(R15), Y17",
            "\tVMOVSS (AX), X28",
            "\tVMOVSS (BX), X29",
            "\tVPXORD Y26, Y26, Y26",
            "\tVPDPBUSD Y16, Y25, Y26",
            "\tVPXORD Y24, Y24, Y24",
            "\tVPDPBUSD Y17, Y25, Y24",
        ]
        for half in range(2):
            aoff = half * 128
            for i in range(4):
                lines.append(f"\tVMOVDQU32 {aoff + i * 32}(SI), Y{18 + i}")
            if half == 0:
                lines.append("\tPREFETCHT0 128(SI)")
            for i in range(4):
                r = half * 4 + i
                lines += [
                    "\tVPXORD Y22, Y22, Y22",
                    f"\tVPDPBUSD Y16, Y{18 + i}, Y22",
                    "\tVPSUBD Y26, Y22, Y22",
                    "\tVCVTDQ2PS Y22, Y22",
                    f"\tVMULSS {r * 4}(R8), X28, X27",
                    "\tVBROADCASTSS X27, Y23",
                    f"\tVFMADD231PS Y23, Y22, Y{r}",
                ]
            for i in range(4):
                r = half * 4 + i
                lines += [
                    "\tVPXORD Y22, Y22, Y22",
                    f"\tVPDPBUSD Y17, Y{18 + i}, Y22",
                    "\tVPSUBD Y24, Y22, Y22",
                    "\tVCVTDQ2PS Y22, Y22",
                    f"\tVMULSS {r * 4}(R8), X29, X27",
                    "\tVBROADCASTSS X27, Y23",
                    f"\tVFMADD231PS Y23, Y22, Y{8 + r}",
                ]
        lines += [
            "\tADDQ $34, DI",
            "\tADDQ $34, R15",
            "\tADDQ $4, AX",
            "\tADDQ $4, BX",
            "\tADDQ $256, SI",
            "\tADDQ $32, R8",
        ]
        return "\n".join(lines)

    text = f"\tMOVQ $8, R10\n{label}:\n"
    text += block() + "\n" + block() + "\n"
    text += f"\tDECQ R10\n\tJNZ  {label}\n"
    return text


def _k512_vnni_store_prologue(row_calc: str) -> str:
    return f"""\tMOVQ out+0(FP), R11
\tMOVQ m+64(FP), AX
\tMOVQ n+72(FP), BX
{row_calc}\tADDQ BX, AX
\tSHLQ $2, AX
\tADDQ AX, R11
\tMOVQ R11, 0(SP)

\tMOVL $0x80808080, CX
\tMOVL CX, 8(SP)
\tVPBROADCASTD 8(SP), Y25

\tMOVQ aQ+8(FP), SI
\tMOVQ aS+16(FP), R8
\tMOVQ data+24(FP), DI
\tMOVQ sB0+32(FP), AX
\tMOVQ sB1+40(FP), BX
\tADDQ off0+48(FP), DI
\tMOVQ data+24(FP), R15
\tADDQ off1+56(FP), R15

""" + "".join(f"\tVXORPS Y{i}, Y{i}, Y{i}\n" for i in range(16))


def gen_q8u_q8s_dual8_accum_vnni_k512() -> str:
    """Out-proj: 8A×2B K=512 + bias + residual accum, N=512 (row stride 512)."""
    text = """// func q8uQ8sDual8AccumVNNIK512N512(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// Out-proj VNNI: block-major A_u8(+128) × B_s8, compensated. N=512 K=512.
// Frame: out+0 aQ+8 aS+16 data+24 sB0+32 sB1+40 off0+48 off1+56 m+64 n+72 bn0+80 bn1+84 → 88
// Stack $16: 0(SP)=out base, 8(SP)=0x80 const.
TEXT ·q8uQ8sDual8AccumVNNIK512N512(SB), NOSPLIT, $16-88
"""
    text += _k512_vnni_store_prologue("\tSHLQ $9, AX // m*512\n")
    text += _k512_vnni_dual8_loop("q8vnni_k512acc_loop")
    text += "\tMOVQ 0(SP), R11\n"
    for r in range(8):
        off = r * 2048
        text += _vnni_hsum8_f32_to_x28(r) + "\n"
        text += f"\tVADDSS bn0+80(FP), X28, X28\n"
        text += f"\tVADDSS {off}(R11), X28, X28\n"
        text += f"\tVMOVSS X28, {off}(R11)\n"
        text += _vnni_hsum8_f32_to_x28(8 + r) + "\n"
        text += f"\tVADDSS bn1+84(FP), X28, X28\n"
        text += f"\tVADDSS {off + 4}(R11), X28, X28\n"
        text += f"\tVMOVSS X28, {off + 4}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8u_q8s_dual8_plain_vnni_k512_n1536() -> str:
    """QKV: 8A×2B K=512 + bias store, N=1536 (row stride 1536)."""
    text = """// func q8uQ8sDual8PlainVNNIK512N1536(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// QKV VNNI: block-major A_u8(+128) × B_s8, compensated. N=1536 K=512.
// Frame: out+0 aQ+8 aS+16 data+24 sB0+32 sB1+40 off0+48 off1+56 m+64 n+72 bn0+80 bn1+84 → 88
// Stack $16: 0(SP)=out base, 8(SP)=0x80 const.
TEXT ·q8uQ8sDual8PlainVNNIK512N1536(SB), NOSPLIT, $16-88
"""
    text += _k512_vnni_store_prologue("\tLEAQ (AX)(AX*2), AX // m*3\n\tSHLQ $9, AX // m*1536\n")
    text += _k512_vnni_dual8_loop("q8vnni_k512pl1536_loop")
    text += "\tMOVQ 0(SP), R11\n"
    for r in range(8):
        off = r * 6144
        text += _vnni_hsum8_f32_to_x28(r) + "\n"
        text += f"\tVADDSS bn0+80(FP), X28, X28\n"
        text += f"\tVMOVSS X28, {off}(R11)\n"
        text += _vnni_hsum8_f32_to_x28(8 + r) + "\n"
        text += f"\tVADDSS bn1+84(FP), X28, X28\n"
        text += f"\tVMOVSS X28, {off + 4}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8u_q8s_dual8_relu_vnni_k512_n2048() -> str:
    """FFN up: 8A×2B K=512 + bias + ReLU store, N=2048 (row stride 2048)."""
    text = """// func q8uQ8sDual8ReLUVNNIK512N2048(out *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// FFN-up VNNI: block-major A_u8(+128) × B_s8, compensated. N=2048 K=512.
// Frame: out+0 aQ+8 aS+16 data+24 sB0+32 sB1+40 off0+48 off1+56 m+64 n+72 bn0+80 bn1+84 → 88
// Stack $16: 0(SP)=out base, 8(SP)=0x80 const.
TEXT ·q8uQ8sDual8ReLUVNNIK512N2048(SB), NOSPLIT, $16-88
"""
    text += _k512_vnni_store_prologue("\tSHLQ $11, AX // m*2048\n")
    text += _k512_vnni_dual8_loop("q8vnni_k512relu_loop")
    text += "\tMOVQ 0(SP), R11\n\tVXORPS X30, X30, X30\n"
    for r in range(8):
        off = r * 8192
        text += _vnni_hsum8_f32_to_x28(r) + "\n"
        text += f"\tVADDSS bn0+80(FP), X28, X28\n"
        text += "\tVMAXSS X30, X28, X28\n"
        text += f"\tVMOVSS X28, {off}(R11)\n"
        text += _vnni_hsum8_f32_to_x28(8 + r) + "\n"
        text += f"\tVADDSS bn1+84(FP), X28, X28\n"
        text += "\tVMAXSS X30, X28, X28\n"
        text += f"\tVMOVSS X28, {off + 4}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8u_q8s_dual8_argmax_vnni_k512() -> str:
    """CTC: 8A×2B K=512 + bias + argmax update of bestV/bestI (8 rows).

    Two-phase epilogue (Go asm UCOMISS is limited to X0-X15):
      phase1 hsum+bias → 16 stack floats; phase2 compares vs bestV with X8 cand.
    """
    text = """// func q8uQ8sDual8ArgmaxVNNIK512(bestV *float32, bestI *int, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, n int, bn0, bn1 float32)
// CTC VNNI: block-major A_u8(+128) × B_s8, compensated, fused argmax. K=512.
// Frame: bestV+0 bestI+8 aQ+16 aS+24 data+32 sB0+40 sB1+48 off0+56 off1+64 n+72 bn0+80 bn1+84 → 88
// Stack $96: 16 hsum+bias floats at 0..63(SP); 0x80 const at 64(SP).
TEXT ·q8uQ8sDual8ArgmaxVNNIK512(SB), NOSPLIT, $96-88
\tMOVQ bestV+0(FP), R11
\tMOVQ bestI+8(FP), R13

\tMOVL $0x80808080, CX
\tMOVL CX, 64(SP)
\tVPBROADCASTD 64(SP), Y25

\tMOVQ aQ+16(FP), SI
\tMOVQ aS+24(FP), R8
\tMOVQ data+32(FP), DI
\tMOVQ sB0+40(FP), AX
\tMOVQ sB1+48(FP), BX
\tADDQ off0+56(FP), DI
\tMOVQ data+32(FP), R15
\tADDQ off1+64(FP), R15

"""
    for i in range(16):
        text += f"\tVXORPS Y{i}, Y{i}, Y{i}\n"
    text += _k512_vnni_dual8_loop("q8vnni_k512amx_loop")
    # phase1: hsum+bias → stack (16 floats, row-major pairs)
    for r in range(8):
        text += _vnni_hsum8_f32_to_x28(r) + "\n"
        text += f"\tVADDSS bn0+80(FP), X28, X28\n"
        text += f"\tVMOVSS X28, {r * 8}(SP)\n"
        text += _vnni_hsum8_f32_to_x28(8 + r) + "\n"
        text += f"\tVADDSS bn1+84(FP), X28, X28\n"
        text += f"\tVMOVSS X28, {r * 8 + 4}(SP)\n"
    # phase2: argmax update (leftmost-max wins on ties, matching the F32 path)
    text += "\tMOVQ n+72(FP), R9\n"
    for r in range(8):
        text += f"\tVMOVSS {r * 4}(R11), X{r}\n"
    for r in range(8):
        lab0 = f"q8amx_r{r}a"
        lab1 = f"q8amx_r{r}b"
        text += f"\tVMOVSS {r * 8}(SP), X8\n"
        text += f"\tUCOMISS X{r}, X8\n"
        text += f"\tJLS  {lab0}\n"
        text += f"\tVMOVAPS X8, X{r}\n"
        text += f"\tMOVQ R9, {r * 8}(R13)\n"
        text += f"{lab0}:\n"
        text += f"\tVMOVSS {r * 8 + 4}(SP), X8\n"
        text += f"\tUCOMISS X{r}, X8\n"
        text += f"\tJLS  {lab1}\n"
        text += f"\tVMOVAPS X8, X{r}\n"
        text += f"\tLEAQ 1(R9), R12\n"
        text += f"\tMOVQ R12, {r * 8}(R13)\n"
        text += f"{lab1}:\n"
    for r in range(8):
        text += f"\tVMOVSS X{r}, {r * 4}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8u_q8s_dual8_logits_vnni_k512() -> str:
    """CTC stage 1: 8A×2B K=512 + bias → 16 approx logits to a vals buffer.

    Same loop as q8uQ8sDual8ArgmaxVNNIK512; the epilogue stores the
    hsum+bias values (rows×2 cols) instead of a fused argmax update. Go
    collects top candidates from the buffer and re-scores them exactly, so
    the final argmax is bit-identical to the full F32 path.
    """
    text = """// func q8uQ8sDual8LogitsVNNIK512(vals *float32, aQ *int8, aS *float32, data *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// CTC VNNI stage 1: A_u8(+128) × B_s8 compensated → 16 approx logits to vals.
// vals layout: [row r][col j] at vals[r*8 + j*4] (r=0..7, j=0..1). K=512.
// Frame: vals+0 aQ+8 aS+16 data+24 sB0+32 sB1+40 off0+48 off1+56 m+64 n+72 bn0+80 bn1+84 → 88
// Stack $72: 0x80 const at 64(SP). m/n unused (kept for call-site symmetry).
TEXT ·q8uQ8sDual8LogitsVNNIK512(SB), NOSPLIT, $72-88
\tMOVQ vals+0(FP), R11

\tMOVL $0x80808080, CX
\tMOVL CX, 64(SP)
\tVPBROADCASTD 64(SP), Y25

\tMOVQ aQ+8(FP), SI
\tMOVQ aS+16(FP), R8
\tMOVQ data+24(FP), DI
\tMOVQ sB0+32(FP), AX
\tMOVQ sB1+40(FP), BX
\tADDQ off0+48(FP), DI
\tMOVQ data+24(FP), R15
\tADDQ off1+56(FP), R15

"""
    for i in range(16):
        text += f"\tVXORPS Y{i}, Y{i}, Y{i}\n"
    text += _k512_vnni_dual8_loop("q8vnni_k512lg_loop")
    for r in range(8):
        text += _vnni_hsum8_f32_to_x28(r) + "\n"
        text += f"\tVADDSS bn0+80(FP), X28, X28\n"
        text += f"\tVMOVSS X28, {r * 8}(R11)\n"
        text += _vnni_hsum8_f32_to_x28(8 + r) + "\n"
        text += f"\tVADDSS bn1+84(FP), X28, X28\n"
        text += f"\tVMOVSS X28, {r * 8 + 4}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_multidot4_generic16() -> str:
    """PP-OCR 4A generic-K kernels (K positive multiple of 8), kept verbatim.

    Main loop consumes K/32 double-ZMM trips; the K%32 remainder is
    handled by full-width 16-float and zero-padded 8-float tails, so
    K=8/16/24/40/... work (dual enters the tails directly; triple callers
    guarantee K>=32).  Folded in here so regeneration reproduces the
    full file.
    """
    return "// func multiDot4TripleBAVX512Generic16(out *[12]float32, a, b0, b1, b2 *float32, K int)\n// 4 A x 3 B, K a positive multiple of 8 with K >= 32 (K%32 remainder is\n// handled by full-width 16 and zero-padded 8 tails).  PP-OCR uses\n// K=48/96/120/192/384/768 here.\n// The AVX2 generic kernel is a dominant detector/recognizer hotspot on Zen4;\n// this keeps all twelve output accumulators in ZMM registers and shares each\n// 16-float A load across three B columns.\n// Frame: out+0, a+8, b0+16, b1+24, b2+32, K+40\nTEXT ·multiDot4TripleBAVX512Generic16(SB), NOSPLIT, $0-48\n\tMOVQ out+0(FP), R11\n\tMOVQ a+8(FP), SI\n\tMOVQ b0+16(FP), DI\n\tMOVQ b1+24(FP), R15\n\tMOVQ b2+32(FP), R14\n\tMOVQ K+40(FP), CX\n\n\tMOVQ CX, AX\n\tSHLQ $2, AX\n\tMOVQ SI, R8\n\tLEAQ (SI)(AX*1), R9\n\tLEAQ (R9)(AX*1), R10\n\tLEAQ (R10)(AX*1), R12\n\n\tVXORPS Z0, Z0, Z0\n\tVXORPS Z1, Z1, Z1\n\tVXORPS Z2, Z2, Z2\n\tVXORPS Z3, Z3, Z3\n\tVXORPS Z4, Z4, Z4\n\tVXORPS Z5, Z5, Z5\n\tVXORPS Z6, Z6, Z6\n\tVXORPS Z7, Z7, Z7\n\tVXORPS Z8, Z8, Z8\n\tVXORPS Z9, Z9, Z9\n\tVXORPS Z10, Z10, Z10\n\tVXORPS Z11, Z11, Z11\n\n\t// PP-OCR's selected widths are all multiples of 32.  Process two ZMM\n\t// chunks per trip to halve loop/AGU overhead while the twelve independent\n\t// FMA dependency chains hide the extra load latency.\n\tMOVQ CX, R13\n\tSHRQ $5, R13\n\ntri4_generic16_loop:\n\tVMOVUPS (DI), Z13\n\tVMOVUPS (R15), Z14\n\tVMOVUPS (R14), Z15\n\tVMOVUPS (R8), Z12\n\tVFMADD231PS Z12, Z13, Z0\n\tVFMADD231PS Z12, Z14, Z4\n\tVFMADD231PS Z12, Z15, Z8\n\tVMOVUPS (R9), Z12\n\tVFMADD231PS Z12, Z13, Z1\n\tVFMADD231PS Z12, Z14, Z5\n\tVFMADD231PS Z12, Z15, Z9\n\tVMOVUPS (R10), Z12\n\tVFMADD231PS Z12, Z13, Z2\n\tVFMADD231PS Z12, Z14, Z6\n\tVFMADD231PS Z12, Z15, Z10\n\tVMOVUPS (R12), Z12\n\tVFMADD231PS Z12, Z13, Z3\n\tVFMADD231PS Z12, Z14, Z7\n\tVFMADD231PS Z12, Z15, Z11\n\tVMOVUPS 64(DI), Z13\n\tVMOVUPS 64(R15), Z14\n\tVMOVUPS 64(R14), Z15\n\tVMOVUPS 64(R8), Z12\n\tVFMADD231PS Z12, Z13, Z0\n\tVFMADD231PS Z12, Z14, Z4\n\tVFMADD231PS Z12, Z15, Z8\n\tVMOVUPS 64(R9), Z12\n\tVFMADD231PS Z12, Z13, Z1\n\tVFMADD231PS Z12, Z14, Z5\n\tVFMADD231PS Z12, Z15, Z9\n\tVMOVUPS 64(R10), Z12\n\tVFMADD231PS Z12, Z13, Z2\n\tVFMADD231PS Z12, Z14, Z6\n\tVFMADD231PS Z12, Z15, Z10\n\tVMOVUPS 64(R12), Z12\n\tVFMADD231PS Z12, Z13, Z3\n\tVFMADD231PS Z12, Z14, Z7\n\tVFMADD231PS Z12, Z15, Z11\n\tADDQ $128, DI\n\tADDQ $128, R15\n\tADDQ $128, R14\n\tADDQ $128, R8\n\tADDQ $128, R9\n\tADDQ $128, R10\n\tADDQ $128, R12\n\tDECQ R13\n\tJNZ tri4_generic16_loop\n\n\t// Tail: K%32 in {16, 24} (e.g. K=48 -> 16, K=120 -> 24). CX still holds\n\t// K. First a full-width 16-float chunk when bit 4 is set, then an\n\t// 8-float chunk zero-padded to 16 lanes when bit 3 is set. The padded\n\t// lanes multiply A rows by 0.0, so the ZMM accumulators are unchanged in\n\t// lanes 8-15. The padded operand is built with VINSERTF64X4 into a zero\n\t// ZMM rather than a YMM FMA: EVEX.256 ops zero the upper half of the\n\t// destination ZMM, which would clobber the accumulators.\n\tTESTQ $16, CX\n\tJZ tri4_noTail16\n\tVMOVUPS (DI), Z13\n\tVMOVUPS (R15), Z14\n\tVMOVUPS (R14), Z15\n\tVMOVUPS (R8), Z12\n\tVFMADD231PS Z12, Z13, Z0\n\tVFMADD231PS Z12, Z14, Z4\n\tVFMADD231PS Z12, Z15, Z8\n\tVMOVUPS (R9), Z12\n\tVFMADD231PS Z12, Z13, Z1\n\tVFMADD231PS Z12, Z14, Z5\n\tVFMADD231PS Z12, Z15, Z9\n\tVMOVUPS (R10), Z12\n\tVFMADD231PS Z12, Z13, Z2\n\tVFMADD231PS Z12, Z14, Z6\n\tVFMADD231PS Z12, Z15, Z10\n\tVMOVUPS (R12), Z12\n\tVFMADD231PS Z12, Z13, Z3\n\tVFMADD231PS Z12, Z14, Z7\n\tVFMADD231PS Z12, Z15, Z11\n\tADDQ $64, DI\n\tADDQ $64, R15\n\tADDQ $64, R14\n\tADDQ $64, R8\n\tADDQ $64, R9\n\tADDQ $64, R10\n\tADDQ $64, R12\ntri4_noTail16:\n\tTESTQ $8, CX\n\tJZ tri4_noTail8\n\tVXORPS Z16, Z16, Z16\n\tVMOVUPS (DI), Y13\n\tVINSERTF64X4 $0, Y13, Z16, Z13\n\tVMOVUPS (R15), Y14\n\tVINSERTF64X4 $0, Y14, Z16, Z14\n\tVMOVUPS (R14), Y15\n\tVINSERTF64X4 $0, Y15, Z16, Z15\n\tVMOVUPS (R8), Y12\n\tVINSERTF64X4 $0, Y12, Z16, Z12\n\tVFMADD231PS Z12, Z13, Z0\n\tVFMADD231PS Z12, Z14, Z4\n\tVFMADD231PS Z12, Z15, Z8\n\tVMOVUPS (R9), Y12\n\tVINSERTF64X4 $0, Y12, Z16, Z12\n\tVFMADD231PS Z12, Z13, Z1\n\tVFMADD231PS Z12, Z14, Z5\n\tVFMADD231PS Z12, Z15, Z9\n\tVMOVUPS (R10), Y12\n\tVINSERTF64X4 $0, Y12, Z16, Z12\n\tVFMADD231PS Z12, Z13, Z2\n\tVFMADD231PS Z12, Z14, Z6\n\tVFMADD231PS Z12, Z15, Z10\n\tVMOVUPS (R12), Y12\n\tVINSERTF64X4 $0, Y12, Z16, Z12\n\tVFMADD231PS Z12, Z13, Z3\n\tVFMADD231PS Z12, Z14, Z7\n\tVFMADD231PS Z12, Z15, Z11\ntri4_noTail8:\n\tVEXTRACTF32X8 $1, Z0, Y12\n\tVADDPS Y0, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 0(R11)\n\tVEXTRACTF32X8 $1, Z1, Y12\n\tVADDPS Y1, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 4(R11)\n\tVEXTRACTF32X8 $1, Z2, Y12\n\tVADDPS Y2, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 8(R11)\n\tVEXTRACTF32X8 $1, Z3, Y12\n\tVADDPS Y3, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 12(R11)\n\tVEXTRACTF32X8 $1, Z4, Y12\n\tVADDPS Y4, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 16(R11)\n\tVEXTRACTF32X8 $1, Z5, Y12\n\tVADDPS Y5, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 20(R11)\n\tVEXTRACTF32X8 $1, Z6, Y12\n\tVADDPS Y6, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 24(R11)\n\tVEXTRACTF32X8 $1, Z7, Y12\n\tVADDPS Y7, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 28(R11)\n\tVEXTRACTF32X8 $1, Z8, Y12\n\tVADDPS Y8, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 32(R11)\n\tVEXTRACTF32X8 $1, Z9, Y12\n\tVADDPS Y9, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 36(R11)\n\tVEXTRACTF32X8 $1, Z10, Y12\n\tVADDPS Y10, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 40(R11)\n\tVEXTRACTF32X8 $1, Z11, Y12\n\tVADDPS Y11, Y12, Y12\n\tVEXTRACTF128 $1, Y12, X13\n\tVADDPS X12, X13, X12\n\tVSHUFPD $1, X12, X12, X13\n\tVADDPS X12, X13, X12\n\tVMOVSHDUP X12, X13\n\tVADDSS X12, X13, X12\n\tVMOVSS X12, 44(R11)\n\tVZEROUPPER\n\tRET\n\n// func multiDot4DualBAVX512Generic16(out *[8]float32, a, b0, b1 *float32, K int)\n// 4 A x 2 B, K a positive multiple of 8 with K >= 32 (16/8-wide tails for\n// the K%32 remainder).  The PP-OCR pointwise projections use 96/192/384,\n// all divisible by 32.  Eight independent ZMM\n// accumulators make this substantially cheaper than the AVX2 8-wide walk.\n// Frame: out+0, a+8, b0+16, b1+24, K+32\nTEXT ·multiDot4DualBAVX512Generic16(SB), NOSPLIT, $0-40\n\tMOVQ out+0(FP), R11\n\tMOVQ a+8(FP), SI\n\tMOVQ b0+16(FP), DI\n\tMOVQ b1+24(FP), R15\n\tMOVQ K+32(FP), CX\n\n\tMOVQ CX, AX\n\tSHLQ $2, AX\n\tMOVQ SI, R8\n\tLEAQ (SI)(AX*1), R9\n\tLEAQ (R9)(AX*1), R10\n\tLEAQ (R10)(AX*1), R12\n\n\tVXORPS Z0, Z0, Z0\n\tVXORPS Z1, Z1, Z1\n\tVXORPS Z2, Z2, Z2\n\tVXORPS Z3, Z3, Z3\n\tVXORPS Z4, Z4, Z4\n\tVXORPS Z5, Z5, Z5\n\tVXORPS Z6, Z6, Z6\n\tVXORPS Z7, Z7, Z7\n\n\tMOVQ CX, R13\n\tSHRQ $5, R13\n\tTESTQ R13, R13\n\tJZ    dual4_tailStart // K < 32: empty main loop, tails consume everything\n\ndual4_generic16_loop:\n\tVMOVUPS (DI), Z9\n\tVMOVUPS (R15), Z10\n\tVMOVUPS (R8), Z8\n\tVFMADD231PS Z8, Z9, Z0\n\tVFMADD231PS Z8, Z10, Z4\n\tVMOVUPS (R9), Z8\n\tVFMADD231PS Z8, Z9, Z1\n\tVFMADD231PS Z8, Z10, Z5\n\tVMOVUPS (R10), Z8\n\tVFMADD231PS Z8, Z9, Z2\n\tVFMADD231PS Z8, Z10, Z6\n\tVMOVUPS (R12), Z8\n\tVFMADD231PS Z8, Z9, Z3\n\tVFMADD231PS Z8, Z10, Z7\n\tVMOVUPS 64(DI), Z9\n\tVMOVUPS 64(R15), Z10\n\tVMOVUPS 64(R8), Z8\n\tVFMADD231PS Z8, Z9, Z0\n\tVFMADD231PS Z8, Z10, Z4\n\tVMOVUPS 64(R9), Z8\n\tVFMADD231PS Z8, Z9, Z1\n\tVFMADD231PS Z8, Z10, Z5\n\tVMOVUPS 64(R10), Z8\n\tVFMADD231PS Z8, Z9, Z2\n\tVFMADD231PS Z8, Z10, Z6\n\tVMOVUPS 64(R12), Z8\n\tVFMADD231PS Z8, Z9, Z3\n\tVFMADD231PS Z8, Z10, Z7\n\tADDQ $128, DI\n\tADDQ $128, R15\n\tADDQ $128, R8\n\tADDQ $128, R9\n\tADDQ $128, R10\n\tADDQ $128, R12\n\tDECQ R13\n\tJNZ dual4_generic16_loop\n\n\t// Tail: K%32 in {16, 24} (K=48 -> 16, K=120 -> 24; K<32 enters here\n\t// directly via dual4_tailStart). CX still holds K.\n\t// See the triple kernel's tail comment for the zero-padding rationale.\ndual4_tailStart:\n\tTESTQ $16, CX\n\tJZ dual4_noTail16\n\tVMOVUPS (DI), Z9\n\tVMOVUPS (R15), Z10\n\tVMOVUPS (R8), Z8\n\tVFMADD231PS Z8, Z9, Z0\n\tVFMADD231PS Z8, Z10, Z4\n\tVMOVUPS (R9), Z8\n\tVFMADD231PS Z8, Z9, Z1\n\tVFMADD231PS Z8, Z10, Z5\n\tVMOVUPS (R10), Z8\n\tVFMADD231PS Z8, Z9, Z2\n\tVFMADD231PS Z8, Z10, Z6\n\tVMOVUPS (R12), Z8\n\tVFMADD231PS Z8, Z9, Z3\n\tVFMADD231PS Z8, Z10, Z7\n\tADDQ $64, DI\n\tADDQ $64, R15\n\tADDQ $64, R8\n\tADDQ $64, R9\n\tADDQ $64, R10\n\tADDQ $64, R12\ndual4_noTail16:\n\tTESTQ $8, CX\n\tJZ dual4_noTail8\n\tVXORPS Z11, Z11, Z11\n\tVMOVUPS (DI), Y9\n\tVINSERTF64X4 $0, Y9, Z11, Z9\n\tVMOVUPS (R15), Y10\n\tVINSERTF64X4 $0, Y10, Z11, Z10\n\tVMOVUPS (R8), Y8\n\tVINSERTF64X4 $0, Y8, Z11, Z8\n\tVFMADD231PS Z8, Z9, Z0\n\tVFMADD231PS Z8, Z10, Z4\n\tVMOVUPS (R9), Y8\n\tVINSERTF64X4 $0, Y8, Z11, Z8\n\tVFMADD231PS Z8, Z9, Z1\n\tVFMADD231PS Z8, Z10, Z5\n\tVMOVUPS (R10), Y8\n\tVINSERTF64X4 $0, Y8, Z11, Z8\n\tVFMADD231PS Z8, Z9, Z2\n\tVFMADD231PS Z8, Z10, Z6\n\tVMOVUPS (R12), Y8\n\tVINSERTF64X4 $0, Y8, Z11, Z8\n\tVFMADD231PS Z8, Z9, Z3\n\tVFMADD231PS Z8, Z10, Z7\ndual4_noTail8:\n\tVEXTRACTF32X8 $1, Z0, Y8\n\tVADDPS Y0, Y8, Y8\n\tVEXTRACTF128 $1, Y8, X9\n\tVADDPS X8, X9, X8\n\tVSHUFPD $1, X8, X8, X9\n\tVADDPS X8, X9, X8\n\tVMOVSHDUP X8, X9\n\tVADDSS X8, X9, X8\n\tVMOVSS X8, 0(R11)\n\tVEXTRACTF32X8 $1, Z1, Y8\n\tVADDPS Y1, Y8, Y8\n\tVEXTRACTF128 $1, Y8, X9\n\tVADDPS X8, X9, X8\n\tVSHUFPD $1, X8, X8, X9\n\tVADDPS X8, X9, X8\n\tVMOVSHDUP X8, X9\n\tVADDSS X8, X9, X8\n\tVMOVSS X8, 4(R11)\n\tVEXTRACTF32X8 $1, Z2, Y8\n\tVADDPS Y2, Y8, Y8\n\tVEXTRACTF128 $1, Y8, X9\n\tVADDPS X8, X9, X8\n\tVSHUFPD $1, X8, X8, X9\n\tVADDPS X8, X9, X8\n\tVMOVSHDUP X8, X9\n\tVADDSS X8, X9, X8\n\tVMOVSS X8, 8(R11)\n\tVEXTRACTF32X8 $1, Z3, Y8\n\tVADDPS Y3, Y8, Y8\n\tVEXTRACTF128 $1, Y8, X9\n\tVADDPS X8, X9, X8\n\tVSHUFPD $1, X8, X8, X9\n\tVADDPS X8, X9, X8\n\tVMOVSHDUP X8, X9\n\tVADDSS X8, X9, X8\n\tVMOVSS X8, 12(R11)\n\tVEXTRACTF32X8 $1, Z4, Y8\n\tVADDPS Y4, Y8, Y8\n\tVEXTRACTF128 $1, Y8, X9\n\tVADDPS X8, X9, X8\n\tVSHUFPD $1, X8, X8, X9\n\tVADDPS X8, X9, X8\n\tVMOVSHDUP X8, X9\n\tVADDSS X8, X9, X8\n\tVMOVSS X8, 16(R11)\n\tVEXTRACTF32X8 $1, Z5, Y8\n\tVADDPS Y5, Y8, Y8\n\tVEXTRACTF128 $1, Y8, X9\n\tVADDPS X8, X9, X8\n\tVSHUFPD $1, X8, X8, X9\n\tVADDPS X8, X9, X8\n\tVMOVSHDUP X8, X9\n\tVADDSS X8, X9, X8\n\tVMOVSS X8, 20(R11)\n\tVEXTRACTF32X8 $1, Z6, Y8\n\tVADDPS Y6, Y8, Y8\n\tVEXTRACTF128 $1, Y8, X9\n\tVADDPS X8, X9, X8\n\tVSHUFPD $1, X8, X8, X9\n\tVADDPS X8, X9, X8\n\tVMOVSHDUP X8, X9\n\tVADDSS X8, X9, X8\n\tVMOVSS X8, 24(R11)\n\tVEXTRACTF32X8 $1, Z7, Y8\n\tVADDPS Y7, Y8, Y8\n\tVEXTRACTF128 $1, Y8, X9\n\tVADDPS X8, X9, X8\n\tVSHUFPD $1, X8, X8, X9\n\tVADDPS X8, X9, X8\n\tVMOVSHDUP X8, X9\n\tVADDSS X8, X9, X8\n\tVMOVSS X8, 28(R11)\n\tVZEROUPPER\n\tRET\n"


# --- Q8R: per-row-scale int-accumulate VNNI (K=512) --------------------------
#
# The per-block-epilogue kernels above pay cvt+scale+fma per (row, B, block),
# which caps them near the F32 multiDot rate. These kernels instead accumulate
# VPDPBUSD int32 across ALL 16 blocks per (row, B) and scale once per panel:
#   total(r,j) = sA_row[r] * sB_row[j] * (acc_int - 128*sumB[j])
# That requires per-row scales on BOTH sides:
#   - B is repacked at load into contiguous s8 rows (Q8R: Q + per-row Scale +
#     pre-multiplied Sum128 = 128*sum(Q row)) from the Q8_0 tensor via
#     round(s8 * sB_block / sB_row), sB_row = max(sB_block).
#   - A is quantized per row (amax over the full K=512 row) with +128 bias.
# Overflow: per dword lane acc <= 16*4*127*255 ≈ 2.07M; |acc - 128*sumB| ≤ 8.4M
# — int32-safe, and < 2^24 so VCVTDQ2PS is exact.

def gen_quantize_panel8_q8srow_k512() -> str:
    """quantizePanel8Q8SRowK512AVX512: 8 A rows K=512, per-row scale.

    q = round(a*127/amax_row)+128 ∈ [1,255]; s[r] = amax/127 (8 floats).
    q layout: block-major [16][8][32] (same as the per-block panel).
    Constants: Z12=abs mask, Z14=127.0, Z6=128.0, X13=1/127, X15=eps.
    """
    text = """// func quantizePanel8Q8SRowK512AVX512(q *int8, s *float32, a *float32)
// 8×512 signed A → +128-biased u8 packs, per-row scale (amax/127).
// q layout: block-major [16][8][32]; s: [8] per row.
// Frame: q+0 s+8 a+16 → 24
TEXT ·quantizePanel8Q8SRowK512AVX512(SB), NOSPLIT, $32-24
\tMOVQ q+0(FP), DI
\tMOVQ s+8(FP), R8
\tMOVQ a+16(FP), SI
\tMOVL $0x42fe0000, AX // 127.0f
\tMOVL AX, 0(SP)
\tVBROADCASTSS 0(SP), Z14
\tMOVL $0x34000000, AX // ~1.2e-7f eps
\tMOVL AX, 4(SP)
\tVMOVSS 4(SP), X15
\tMOVL $0x3c010204, AX // 1/127.0f
\tMOVL AX, 8(SP)
\tVMOVSS 8(SP), X13
\tMOVL $0x7fffffff, AX // abs mask
\tMOVL AX, 12(SP)
\tVBROADCASTSS 12(SP), Z12
\tMOVL $0x43000000, AX // 128.0f bias
\tMOVL AX, 16(SP)
\tVBROADCASTSS 16(SP), Z6

\tMOVQ $8, R10 // rows
\tMOVQ SI, R11 // A ptr (flows across rows)
\tMOVQ DI, R15 // q row base = DI + r*32
\tMOVQ R8, R13 // s ptr
q8sr_row_loop:
\t// amax over the row's 512 floats: 8 batches of 4 ZMM, running max in Z4
"""
    for batch in range(8):
        base = batch * 256
        text += f"""\tVMOVUPS {base}(R11), Z0
\tVMOVUPS {base + 64}(R11), Z1
\tVMOVUPS {base + 128}(R11), Z2
\tVMOVUPS {base + 192}(R11), Z3
\tVANDPS Z12, Z0, Z0
\tVANDPS Z12, Z1, Z1
\tVANDPS Z12, Z2, Z2
\tVANDPS Z12, Z3, Z3
\tVMAXPS Z0, Z1, Z0
\tVMAXPS Z2, Z3, Z2
"""
        if batch == 0:
            text += "\tVMAXPS Z0, Z2, Z4\n"
        else:
            text += "\tVMAXPS Z2, Z0, Z0\n\tVMAXPS Z4, Z0, Z4\n"
    text += """\tVEXTRACTF32X8 $1, Z4, Y1
\tVMAXPS Y4, Y1, Y4
\tVEXTRACTF32X4 $1, Y4, X1
\tVMAXPS X4, X1, X4
\tVSHUFPD $1, X4, X4, X1
\tVMAXPS X4, X1, X4
\tVMOVSHDUP X4, X1
\tVMAXSS X4, X1, X4 // amax
\tVMOVAPS X4, X0
"""
    text += """\tVMULSS X13, X0, X4 // scale = amax/127
\tVMAXSS X15, X0, X3
\tVRCP14SS X3, X3, X3
\tVMULSS X14, X3, X3 // inv = 127/amax
\tVBROADCASTSS X3, Z5
\tVMOVSS X4, (R13)
\tMOVQ R15, R12 // q block ptr for this row
\tMOVQ $16, R14
q8sr_blk_loop:
\tVMOVUPS (R11), Z0
\tVMOVUPS 64(R11), Z1
\tVMULPS Z5, Z0, Z0
\tVMULPS Z5, Z1, Z1
\tVADDPS Z6, Z0, Z0
\tVADDPS Z6, Z1, Z1
\tVCVTPS2DQ Z0, Z0
\tVCVTPS2DQ Z1, Z1
\tVPMOVDB Z0, X4
\tVPMOVDB Z1, X7
\tVMOVUPS X4, (R12)
\tVMOVUPS X7, 16(R12)
\tADDQ $128, R11
\tADDQ $256, R12
\tDECQ R14
\tJNZ  q8sr_blk_loop
\tADDQ $32, R15 // next row's q base
\tADDQ $4, R13 // next s
\tDECQ R10
\tJNZ  q8sr_row_loop
\tVZEROUPPER
\tRET
"""
    return text


def _q8r_dual8_intaccum_loop(label: str) -> str:
    """16-block loop: 8 A × 2 B VPDPBUSD into persistent int32 accums Y0-15.

    No per-block epilogue: scales are per-row, applied once per (r,j) after
    the loop. Registers: Y0-15 int accums, Y16/17 B, Y18-21 A temps.
    GP: SI=A panel R15=B0 R14=B1 R10=counter (A layout [b][8][32]).
    """
    lines = [f"{label}:",
        "\tPREFETCHT0 256(R15)",
        "\tPREFETCHT0 256(R14)",
        "\tPREFETCHT0 512(SI)",
        "\tVMOVDQU32 (R15), Y16",
        "\tVMOVDQU32 (R14), Y17",
    ]
    for half in range(2):
        aoff = half * 128
        for i in range(4):
            lines.append(f"\tVMOVDQU32 {aoff + i * 32}(SI), Y{18 + i}")
        for i in range(4):
            r = half * 4 + i
            lines.append(f"\tVPDPBUSD Y16, Y{18 + i}, Y{r}")
            lines.append(f"\tVPDPBUSD Y17, Y{18 + i}, Y{8 + r}")
    lines += [
        "\tADDQ $32, R15",
        "\tADDQ $32, R14",
        "\tADDQ $256, SI",
        "\tDECQ R10",
        f"\tJNZ  {label}",
    ]
    return "\n".join(lines)


def _q8r_epilogue_hscale_store(r: int, j: int, off: int, n_cols: int, relu: bool, row_bytes: int) -> str:
    """One (row r, B j) epilogue: cvt; hsum; −128*sumB[j] (float, post-hsum);
    ×sA[r]×sB[j]; +bias[; relu]; store at out row offset r*row_bytes (+4 for j=1)."""
    y = j * 8 + r
    sumb = "(R12)" if j == 0 else "(R13)"
    sbs = "(AX)" if j == 0 else "(BX)"
    bn = "bn0+96(FP)" if j == 0 else "bn1+100(FP)"
    lines = [
        f"\tVCVTDQ2PS Y{y}, Y{y}",
    ]
    lines.append(_vnni_hsum8_f32_to_x28(y))
    lines += [
        f"\tVSUBSS {sumb}, X28, X28",
        f"\tVMULSS {r * 4}(R8), X28, X28",
        f"\tVMULSS {sbs}, X28, X28",
        f"\tVADDSS {bn}, X28, X28",
    ]
    if relu:
        lines.append("\tVMAXSS X30, X28, X28")
    lines.append(f"\tVMOVSS X28, {off}(R11)")
    return "\n".join(lines)


def _q8r_prologue(row_calc: str) -> str:
    return f"""\tMOVQ out+0(FP), R11
\tMOVQ m+80(FP), AX
\tMOVQ n+88(FP), BX
{row_calc}\tADDQ BX, AX
\tSHLQ $2, AX
\tADDQ AX, R11

\tMOVQ aQ+8(FP), SI
\tMOVQ aS+16(FP), R8
\tMOVQ qB+24(FP), R15
\tMOVQ qB+24(FP), R14
\tADDQ off0+64(FP), R15
\tADDQ off1+72(FP), R14
\tMOVQ sumB0+48(FP), R12
\tMOVQ sumB1+56(FP), R13
\tMOVQ sB0+32(FP), AX
\tMOVQ sB1+40(FP), BX

""" + "".join(f"\tVPXORD Y{i}, Y{i}, Y{i}\n" for i in range(16)) + "\tMOVQ $16, R10\n"


def gen_q8r_dual8_plain_vnni_k512(n_cols: int) -> str:
    """Q8R int-accumulate plain kernel for N=512 (out-proj) / N=1536 (QKV)."""
    if n_cols == 512:
        row_calc = "\tSHLQ $9, AX // m*512\n"
        row_bytes = 2048
    elif n_cols == 1536:
        row_calc = "\tLEAQ (AX)(AX*2), AX // m*3\n\tSHLQ $9, AX // m*1536\n"
        row_bytes = 6144
    else:
        raise ValueError(f"unsupported N={n_cols}")
    text = f"""// func q8rDual8PlainVNNIK512N{n_cols}(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, sumB0, sumB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// Q8R int-accumulate VNNI: A_u8(+128) × B_s8, per-row scales, plain store. N={n_cols} K=512.
// Frame: out+0 aQ+8 aS+16 qB+24 sB0+32 sB1+40 sumB0+48 sumB1+56 off0+64 off1+72 m+80 n+88 bn0+96 bn1+100 → 104
TEXT ·q8rDual8PlainVNNIK512N{n_cols}(SB), NOSPLIT, $0-104
"""
    text += _q8r_prologue(row_calc)
    text += _q8r_dual8_intaccum_loop(f"q8r_pl{n_cols}_loop") + "\n"
    for r in range(8):
        text += _q8r_epilogue_hscale_store(r, 0, r*row_bytes, n_cols, False, row_bytes) + "\n"
        text += _q8r_epilogue_hscale_store(r, 1, r*row_bytes+4, n_cols, False, row_bytes) + "\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8r_dual8_relu_vnni_k512_n2048() -> str:
    """Q8R int-accumulate ReLU kernel for N=2048 (FFN up)."""
    text = """// func q8rDual8ReLUVNNIK512N2048(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, sumB0, sumB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// Q8R int-accumulate VNNI: A_u8(+128) × B_s8, per-row scales, ReLU store. N=2048 K=512.
// Frame: out+0 aQ+8 aS+16 qB+24 sB0+32 sB1+40 sumB0+48 sumB1+56 off0+64 off1+72 m+80 n+88 bn0+96 bn1+100 → 104
TEXT ·q8rDual8ReLUVNNIK512N2048(SB), NOSPLIT, $0-104
"""
    text += _q8r_prologue("\tSHLQ $11, AX // m*2048\n")
    text += _q8r_dual8_intaccum_loop("q8r_relu2048_loop") + "\n"
    text += "\tVXORPS X30, X30, X30\n"
    for r in range(8):
        text += _q8r_epilogue_hscale_store(r, 0, r*8192, 2048, True, 8192) + "\n"
        text += _q8r_epilogue_hscale_store(r, 1, r*8192+4, 2048, True, 8192) + "\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


# --- Q8Z: ZMM per-block VNNI (K=512) -----------------------------------------
#
# Per-block scales on both sides (text-regression-safe), but ZMM VPDPBUSD
# processes 2 blocks (64B) per instruction and B is repacked scale-stripped
# (no requant → identical numerics to Q8_0). Per 2-block tick per (r,j):
#   pxor + vpdp + psubd(comp) + cvt + mulps(sA-vec) + fma(sB-vec)  = 6 ops
# vs 7 for the YMM per-block kernel at half the MACs — ~1.9× fewer instr/MAC.
# The 16-lane scale vectors [s(2g)×8 | s(2g+1)×8] are built with
# VBROADCASTSS + VINSERTF32X8 (Gemma m3row idiom). B compensation is the
# per-tick VPDPBUSD(B, 0x80) subtracted in the integer domain before cvt.

def gen_quantize_panel8_q8s_k512_zmm() -> str:
    """quantizePanel8Q8SK512ZmmAVX512: 8 A rows K=512, per-block scale, ZMM layout.

    Same math as quantizePanel8Q8SK512AVX512 (q = round(a*127/amax)+128 per
    32-block), but the byte layout interleaves each row's block pairs:
      q[g*512 + r*64 : +32] = block 2g of row r; q[+32 : +64] = block 2g+1.
    Scales s[b*8 + r] keep the block-major layout (kernel builds lane vecs).
    """

    def amax_chain(src: str, tmpy: str, tmpx: str, dst: str) -> list:
        return [
            f"\tVEXTRACTF32X8 $1, {src}, {tmpy}",
            f"\tVMAXPS Y{src[1:]}, {tmpy}, Y{src[1:]}",
            f"\tVEXTRACTF32X4 $1, Y{src[1:]}, X{tmpx}",
            f"\tVMAXPS X{src[1:]}, X{tmpx}, X{src[1:]}",
            f"\tVSHUFPD $1, X{src[1:]}, X{src[1:]}, X{tmpx}",
            f"\tVMAXPS X{src[1:]}, X{tmpx}, X{src[1:]}",
            f"\tVMOVSHDUP X{src[1:]}, X{tmpx}",
            f"\tVMAXSS X{src[1:]}, X{tmpx}, X{dst}",
        ]

    def dual_block(a_off: int, q_off: int, s_off: int) -> str:
        # rows r (R11) and r+1 (R15); one 32-float block each.
        am0 = "(R11)" if a_off == 0 else f"{a_off}(R11)"
        am1 = f"{a_off + 64}(R11)"
        bm0 = "(R15)" if a_off == 0 else f"{a_off}(R15)"
        bm1 = f"{a_off + 64}(R15)"
        qm0 = "(R12)" if q_off == 0 else f"{q_off}(R12)"
        qm0b = f"{q_off + 16}(R12)"
        qm1 = f"{q_off + 64}(R12)"   # row r+1 in the [g][8][64] tile
        qm1b = f"{q_off + 80}(R12)"
        sm0 = "(R13)" if s_off == 0 else f"{s_off}(R13)"
        sm1 = f"{s_off + 4}(R13)"
        lines = [
            f"\tVMOVUPS {am0}, Z0",
            f"\tVMOVUPS {am1}, Z1",
            f"\tVMOVUPS {bm0}, Z8",
            f"\tVMOVUPS {bm1}, Z9",
            "\tVANDPS Z12, Z0, Z2",
            "\tVANDPS Z12, Z1, Z3",
            "\tVANDPS Z12, Z8, Z10",
            "\tVANDPS Z12, Z9, Z11",
            "\tVMAXPS Z2, Z3, Z2",
            "\tVMAXPS Z10, Z11, Z10",
        ]
        lines += amax_chain("Z2", "Y3", "3", "2")
        lines += amax_chain("Z10", "Y11", "11", "10")
        lines += [
            "\tVMULSS X13, X2, X4",
            "\tVMAXSS X15, X2, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            f"\tVMOVSS X4, {sm0}",
            "\tVMULPS Z5, Z0, Z0",
            "\tVMULPS Z5, Z1, Z1",
            "\tVADDPS Z6, Z0, Z0",
            "\tVADDPS Z6, Z1, Z1",
            "\tVCVTPS2DQ Z0, Z0",
            "\tVCVTPS2DQ Z1, Z1",
            "\tVPMOVDB Z0, X4",
            "\tVPMOVDB Z1, X7",
            f"\tVMOVUPS X4, {qm0}",
            f"\tVMOVUPS X7, {qm0b}",
            "\tVMULSS X13, X10, X4",
            "\tVMAXSS X15, X10, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            f"\tVMOVSS X4, {sm1}",
            "\tVMULPS Z5, Z8, Z8",
            "\tVMULPS Z5, Z9, Z9",
            "\tVADDPS Z6, Z8, Z8",
            "\tVADDPS Z6, Z9, Z9",
            "\tVCVTPS2DQ Z8, Z8",
            "\tVCVTPS2DQ Z9, Z9",
            "\tVPMOVDB Z8, X4",
            "\tVPMOVDB Z9, X7",
            f"\tVMOVUPS X4, {qm1}",
            f"\tVMOVUPS X7, {qm1b}",
        ]
        return "\n".join(lines)

    text = """// func quantizePanel8Q8SK512ZmmAVX512(q *int8, s *float32, sA *float32, a *float32)
// 8×512 signed A → +128-biased u8 packs [g][8][64] + block-major f32 scales,
// plus prearranged per-tick lane vecs sA[g*128 + r*16] = [s(2g,r)×8|s(2g+1,r)×8].
// Frame: q+0 s+8 sA+16 a+24 → 32
TEXT ·quantizePanel8Q8SK512ZmmAVX512(SB), NOSPLIT, $32-32
\tMOVQ q+0(FP), DI
\tMOVQ s+8(FP), R8
\tMOVQ a+24(FP), SI
\tMOVL $0x42fe0000, AX // 127.0f
\tMOVL AX, 0(SP)
\tVBROADCASTSS 0(SP), Z14
\tMOVL $0x34000000, AX // ~1.2e-7f eps
\tMOVL AX, 4(SP)
\tVMOVSS 4(SP), X15
\tMOVL $0x3c010204, AX // 1/127.0f
\tMOVL AX, 8(SP)
\tVMOVSS 8(SP), X13
\tMOVL $0x7fffffff, AX // abs mask
\tMOVL AX, 12(SP)
\tVBROADCASTSS 12(SP), Z12
\tMOVL $0x43000000, AX // 128.0f bias
\tMOVL AX, 16(SP)
\tVBROADCASTSS 16(SP), Z6

\tMOVQ $4, R9
\tMOVQ $0, R14 // pair index 0..3 → row = pair*2
q8sz_pair_loop:
\tMOVQ $8, R10 // 16/2 blocks = 8 groups
\tMOVQ R14, AX
\tSHLQ $1, AX // r = pair*2
\tMOVQ AX, BX
\tSHLQ $11, BX // r*2048 (512 floats)
\tLEAQ (SI)(BX*1), R11
\tLEAQ 2048(R11), R15
\tMOVQ AX, BX
\tSHLQ $6, BX // r*64 (ZMM tile row stride)
\tLEAQ (DI)(BX*1), R12
\tLEAQ (R8)(AX*4), R13
q8sz_blk_loop:
\tPREFETCHT0 256(R11)
\tPREFETCHT0 256(R15)
"""
    text += dual_block(0, 0, 0) + "\n"
    text += dual_block(128, 32, 32) + "\n"
    text += """\tADDQ $256, R11 // A: 64 floats
\tADDQ $256, R15
\tADDQ $512, R12 // q: 1 group tile (8 rows × 64B)
\tADDQ $64, R13 // s: 2 blocks × 8 floats
\tDECQ R10
\tJNZ  q8sz_blk_loop
\tINCQ R14
\tDECQ R9
\tJNZ  q8sz_pair_loop

\t// Prearrange per-tick sA lane vecs: sA[g*512 + r*64] = [s(2g,r)×8 | s(2g+1,r)×8].
\tMOVQ sA+16(FP), R9
\tMOVQ $0, R10 // g
q8sz_vec_g:
\tMOVQ $0, R11 // r
q8sz_vec_r:
\tMOVQ R10, AX
\tSHLQ $4, AX // g*16
\tADDQ R11, AX // g*16 + r  (s[(2g)*8 + r])
\tVBROADCASTSS (R8)(AX*4), Y0
\tVMOVUPS Y0, (R9)
\tVBROADCASTSS 32(R8)(AX*4), Y0
\tVMOVUPS Y0, 32(R9)
\tADDQ $64, R9
\tINCQ R11
\tCMPQ R11, $8
\tJB   q8sz_vec_r
\tINCQ R10
\tCMPQ R10, $8
\tJB   q8sz_vec_g
\tVZEROUPPER
\tRET
"""
    return text


def _q8z_dual8_tick() -> str:
    """One 2-block tick: 8 A rows × 2 B, ZMM VPDPBUSD + per-block scale vecs.

    Registers: Z0-15 float accums, Z16/17 B, Z18-25 A, Z26 sB1-vec,
    Z27/28 comp0/1, Z29 int/const temp, Z30 sA-vec, Z31 sB0-vec.
    """
    lines = [
        "\tPREFETCHT0 256(R15)",
        "\tPREFETCHT0 256(R14)",
        "\tPREFETCHT0 1024(SI)",
        "\tVMOVDQU64 (R15), Z16",
        "\tVMOVDQU64 (R14), Z17",
        "\tVPXORD Z27, Z27, Z27",
        "\tVPBROADCASTD 0(SP), Z29",
        "\tVPDPBUSD Z16, Z29, Z27",
        "\tVPXORD Z28, Z28, Z28",
        "\tVPBROADCASTD 0(SP), Z29",
        "\tVPDPBUSD Z17, Z29, Z28",
    ]
    for i in range(8):
        lines.append(f"\tVMOVDQU64 {i * 64}(SI), Z{18 + i}")
    # sB0-vec / sB1-vec
    lines += [
        "\tVBROADCASTSS (AX), Y31",
        "\tVBROADCASTSS 4(AX), Y30",
        "\tVINSERTF32X8 $1, Y30, Z31, Z31",
        "\tVBROADCASTSS (BX), Y26",
        "\tVBROADCASTSS 4(BX), Y30",
        "\tVINSERTF32X8 $1, Y30, Z26, Z26",
    ]
    for r in range(8):
        lines += [
            # sA-vec is prearranged in the panel ([g][r][16]: one 64B load).
            f"\tVMOVDQU64 {r * 64}(R8), Z30",
            "\tVPXORD Z29, Z29, Z29",
            f"\tVPDPBUSD Z16, Z{18 + r}, Z29",
            "\tVPSUBD Z27, Z29, Z29",
            "\tVCVTDQ2PS Z29, Z29",
            "\tVMULPS Z30, Z29, Z29",
            f"\tVFMADD231PS Z31, Z29, Z{r}",
            "\tVPXORD Z29, Z29, Z29",
            f"\tVPDPBUSD Z17, Z{18 + r}, Z29",
            "\tVPSUBD Z28, Z29, Z29",
            "\tVCVTDQ2PS Z29, Z29",
            "\tVMULPS Z30, Z29, Z29",
            f"\tVFMADD231PS Z26, Z29, Z{8 + r}",
        ]
    lines += [
        "\tADDQ $64, R15",
        "\tADDQ $64, R14",
        "\tADDQ $8, AX",
        "\tADDQ $8, BX",
        "\tADDQ $512, SI",
        "\tADDQ $512, R8",
    ]
    return "\n".join(lines)


def _q8z_prologue(row_calc: str) -> str:
    return f"""\tMOVQ out+0(FP), R11
\tMOVQ m+64(FP), AX
\tMOVQ n+72(FP), BX
{row_calc}\tADDQ BX, AX
\tSHLQ $2, AX
\tADDQ AX, R11

\tMOVL $0x80808080, CX
\tMOVL CX, 0(SP)
\tVPBROADCASTD 0(SP), Z26

\tMOVQ aQ+8(FP), SI
\tMOVQ aS+16(FP), R8
\tMOVQ qB+24(FP), R15
\tMOVQ qB+24(FP), R14
\tADDQ off0+48(FP), R15
\tADDQ off1+56(FP), R14
\tMOVQ sB0+32(FP), AX
\tMOVQ sB1+40(FP), BX

""" + "".join(f"\tVXORPS Z{i}, Z{i}, Z{i}\n" for i in range(16)) + "\tMOVQ $8, R10\n"


def gen_q8z_dual8_plain_vnni_k512(n_cols: int) -> str:
    """Q8Z ZMM per-block plain kernel for N=512 (out-proj) / N=1536 (QKV)."""
    if n_cols == 512:
        row_calc = "\tSHLQ $9, AX // m*512\n"
        row_bytes = 2048
    elif n_cols == 1536:
        row_calc = "\tLEAQ (AX)(AX*2), AX // m*3\n\tSHLQ $9, AX // m*1536\n"
        row_bytes = 6144
    else:
        raise ValueError(f"unsupported N={n_cols}")
    text = f"""// func q8zDual8PlainVNNIK512N{n_cols}(out *float32, aQ *int8, aS *float32, qB *int8, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// Q8Z ZMM per-block VNNI: A_u8(+128) × B_s8 stripped, per-block scale vecs. N={n_cols} K=512.
// Frame: out+0 aQ+8 aS+16 qB+24 sB0+32 sB1+40 off0+48 off1+56 m+64 n+72 bn0+80 bn1+84 → 88
// Stack $16: 0(SP)=0x80 const. Registers: Z0-15 accums, Z16/17 B, Z18-25 A,
// Z26 sB1-vec, Z27/28 comp, Z29 temp, Z30 sA-vec, Z31 sB0-vec.
TEXT ·q8zDual8PlainVNNIK512N{n_cols}(SB), NOSPLIT, $16-88
"""
    text += _q8z_prologue(row_calc)
    text += "q8z_pl" + str(n_cols) + "_loop:\n" + _q8z_dual8_tick() + "\n"
    text += "\tDECQ R10\n\tJNZ  q8z_pl" + str(n_cols) + "_loop\n"
    for r in range(8):
        for j in range(2):
            y = j * 8 + r
            off = r * row_bytes + j * 4
            bn = "bn0+80(FP)" if j == 0 else "bn1+84(FP)"
            text += hsum_z_to_x28(y) + "\n"
            text += f"\tVADDSS {bn}, X28, X28\n"
            text += f"\tVMOVSS X28, {off}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8z_dual8_relu_vnni_k512_n2048() -> str:
    """Q8Z ZMM per-block ReLU kernel for N=2048 (FFN up)."""
    text = """// func q8zDual8ReLUVNNIK512N2048(out *float32, aQ *int8, aS *float32, qB *int8, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// Q8Z ZMM per-block VNNI: A_u8(+128) × B_s8 stripped, per-block scale vecs. N=2048 K=512.
// Frame: out+0 aQ+8 aS+16 qB+24 sB0+32 sB1+40 off0+48 off1+56 m+64 n+72 bn0+80 bn1+84 → 88
TEXT ·q8zDual8ReLUVNNIK512N2048(SB), NOSPLIT, $16-88
"""
    text += _q8z_prologue("\tSHLQ $11, AX // m*2048\n")
    text += "q8z_relu2048_loop:\n" + _q8z_dual8_tick() + "\n"
    text += "\tDECQ R10\n\tJNZ  q8z_relu2048_loop\n\tVXORPS X30, X30, X30\n"
    for r in range(8):
        for j in range(2):
            y = j * 8 + r
            off = r * 8192 + j * 4
            bn = "bn0+80(FP)" if j == 0 else "bn1+84(FP)"
            text += hsum_z_to_x28(y) + "\n"
            text += f"\tVADDSS {bn}, X28, X28\n"
            text += "\tVMAXSS X30, X28, X28\n"
            text += f"\tVMOVSS X28, {off}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_quantize_panel8_q8u_k2048_zmm() -> str:
    """quantizePanel8Q8UZmmK2048AVX512: 8 A rows K=2048 → u8 (0..127), ZMM layout.

    Same math as quantizePanel8Q8UAVX512 (ReLU A ≥ 0, q ∈ [0..127]), but the
    byte layout interleaves each row's block pairs for ZMM VPDPBUSD:
      q[g*512 + r*64 : +32] = block 2g of row r; q[+32 : +64] = block 2g+1.
    Scales keep the block-major layout s[b*8 + r].
    """

    def dual_block(a_off: int, q_off: int, s_off: int) -> str:
        am0 = "(R11)" if a_off == 0 else f"{a_off}(R11)"
        am1 = f"{a_off + 64}(R11)"
        bm0 = "(R15)" if a_off == 0 else f"{a_off}(R15)"
        bm1 = f"{a_off + 64}(R15)"
        qm0 = "(R12)" if q_off == 0 else f"{q_off}(R12)"
        qm0b = f"{q_off + 16}(R12)"
        qm1 = f"{q_off + 64}(R12)"
        qm1b = f"{q_off + 80}(R12)"
        sm0 = "(R13)" if s_off == 0 else f"{s_off}(R13)"
        sm1 = f"{s_off + 4}(R13)"
        lines = [
            f"\tVMOVUPS {am0}, Z0",
            f"\tVMOVUPS {am1}, Z1",
            f"\tVMOVUPS {bm0}, Z8",
            f"\tVMOVUPS {bm1}, Z9",
            "\tVMAXPS Z0, Z1, Z2",
            "\tVEXTRACTF32X8 $1, Z2, Y3",
            "\tVMAXPS Y2, Y3, Y2",
            "\tVEXTRACTF32X4 $1, Y2, X3",
            "\tVMAXPS X2, X3, X2",
            "\tVSHUFPD $1, X2, X2, X3",
            "\tVMAXPS X2, X3, X2",
            "\tVMOVSHDUP X2, X3",
            "\tVMAXSS X2, X3, X2",
            "\tVMAXPS Z8, Z9, Z10",
            "\tVEXTRACTF32X8 $1, Z10, Y11",
            "\tVMAXPS Y10, Y11, Y10",
            "\tVEXTRACTF32X4 $1, Y10, X11",
            "\tVMAXPS X10, X11, X10",
            "\tVSHUFPD $1, X10, X10, X11",
            "\tVMAXPS X10, X11, X10",
            "\tVMOVSHDUP X10, X11",
            "\tVMAXSS X10, X11, X10",
            "\tVMULSS X13, X2, X4",
            "\tVMAXSS X15, X2, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            f"\tVMOVSS X4, {sm0}",
            "\tVMULPS Z5, Z0, Z0",
            "\tVMULPS Z5, Z1, Z1",
            "\tVCVTPS2DQ Z0, Z0",
            "\tVCVTPS2DQ Z1, Z1",
            "\tVPMOVDB Z0, X6",
            "\tVPMOVDB Z1, X7",
            f"\tVMOVUPS X6, {qm0}",
            f"\tVMOVUPS X7, {qm0b}",
            "\tVMULSS X13, X10, X4",
            "\tVMAXSS X15, X10, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            f"\tVMOVSS X4, {sm1}",
            "\tVMULPS Z5, Z8, Z8",
            "\tVMULPS Z5, Z9, Z9",
            "\tVCVTPS2DQ Z8, Z8",
            "\tVCVTPS2DQ Z9, Z9",
            "\tVPMOVDB Z8, X6",
            "\tVPMOVDB Z9, X7",
            f"\tVMOVUPS X6, {qm1}",
            f"\tVMOVUPS X7, {qm1b}",
        ]
        return "\n".join(lines)

    text = """// func quantizePanel8Q8UZmmK2048AVX512(q *int8, s *float32, a *float32)
// 8×2048 non-neg A → u8 packs [g][8][64] + block-major f32 scales.
// Frame: q+0 s+8 a+16 → 24
TEXT ·quantizePanel8Q8UZmmK2048AVX512(SB), NOSPLIT, $16-24
\tMOVQ q+0(FP), DI
\tMOVQ s+8(FP), R8
\tMOVQ a+16(FP), SI
\tMOVL $0x42fe0000, AX // 127.0f
\tMOVL AX, 0(SP)
\tVBROADCASTSS 0(SP), Z14
\tMOVL $0x34000000, AX // ~1.2e-7f eps
\tMOVL AX, 4(SP)
\tVMOVSS 4(SP), X15
\tMOVL $0x3c010204, AX // 1/127.0f
\tMOVL AX, 8(SP)
\tVMOVSS 8(SP), X13

\tMOVQ $4, R9
\tMOVQ $0, R14 // pair index 0..3 → row = pair*2
q8uz_pair_loop:
\tMOVQ $32, R10 // 64/2 blocks = 32 groups
\tMOVQ R14, AX
\tSHLQ $1, AX // r = pair*2
\tMOVQ AX, BX
\tSHLQ $13, BX // r*8192
\tLEAQ (SI)(BX*1), R11
\tLEAQ 8192(R11), R15
\tMOVQ AX, BX
\tSHLQ $6, BX // r*64 (ZMM tile row stride)
\tLEAQ (DI)(BX*1), R12
\tLEAQ (R8)(AX*4), R13
q8uz_blk_loop:
\tPREFETCHT0 256(R11)
\tPREFETCHT0 256(R15)
"""
    text += dual_block(0, 0, 0) + "\n"
    text += dual_block(128, 32, 32) + "\n"
    text += """\tADDQ $256, R11 // A: 64 floats
\tADDQ $256, R15
\tADDQ $512, R12 // q: 1 group tile (8 rows × 64B)
\tADDQ $64, R13 // s: 2 blocks × 8 floats
\tDECQ R10
\tJNZ  q8uz_blk_loop
\tINCQ R14
\tDECQ R9
\tJNZ  q8uz_pair_loop
\tVZEROUPPER
\tRET
"""
    return text


def _q8z_accum_tick() -> str:
    """One 2-block tick of the K=2048 accum kernel: no compensation (A is u8).

    Same register map as _q8z_dual8_tick minus the comp registers.
    """
    lines = [
        "\tPREFETCHT0 256(R15)",
        "\tPREFETCHT0 256(R14)",
        "\tPREFETCHT0 1024(SI)",
        "\tVMOVDQU64 (R15), Z16",
        "\tVMOVDQU64 (R14), Z17",
    ]
    for i in range(8):
        lines.append(f"\tVMOVDQU64 {i * 64}(SI), Z{18 + i}")
    lines += [
        "\tVBROADCASTSS (AX), Y31",
        "\tVBROADCASTSS 4(AX), Y30",
        "\tVINSERTF32X8 $1, Y30, Z31, Z31",
        "\tVBROADCASTSS (BX), Y26",
        "\tVBROADCASTSS 4(BX), Y30",
        "\tVINSERTF32X8 $1, Y30, Z26, Z26",
    ]
    for r in range(8):
        lines += [
            f"\tVBROADCASTSS {r * 4}(R8), Y30",
            f"\tVBROADCASTSS {32 + r * 4}(R8), Y29",
            "\tVINSERTF32X8 $1, Y29, Z30, Z30",
            "\tVPXORD Z29, Z29, Z29",
            f"\tVPDPBUSD Z16, Z{18 + r}, Z29",
            "\tVCVTDQ2PS Z29, Z29",
            "\tVMULPS Z30, Z29, Z29",
            f"\tVFMADD231PS Z31, Z29, Z{r}",
            "\tVPXORD Z29, Z29, Z29",
            f"\tVPDPBUSD Z17, Z{18 + r}, Z29",
            "\tVCVTDQ2PS Z29, Z29",
            "\tVMULPS Z30, Z29, Z29",
            f"\tVFMADD231PS Z26, Z29, Z{8 + r}",
        ]
    lines += [
        "\tADDQ $64, R15",
        "\tADDQ $64, R14",
        "\tADDQ $8, AX",
        "\tADDQ $8, BX",
        "\tADDQ $512, SI",
        "\tADDQ $64, R8",
    ]
    return "\n".join(lines)


def gen_q8z_row_dual_vnni_k512() -> str:
    """q8zRowDualVNNIK512: ONE A row × 2 B rows (n, n+1) from a pre-offset panel.

    Tail path for M%8==1: writes out[0], out[1] directly (cols n, n+1 of one
    row). Same per-tick math as the dual8 kernel (signed A, +128 comp) with a
    single accumulator pair Z0 (B0), Z1 (B1). aQ is pre-offset to the row
    (panel base + r*64); the [g][8][64] tile stride (512B) makes per-tick A
    loads land on group g of that row.
    """
    label = "q8z_row_loop"
    lines = [f"{label}:"]
    lines += [
        "\tPREFETCHT0 256(R15)",
        "\tPREFETCHT0 256(R14)",
        "\tPREFETCHT0 512(SI)",
        "\tVMOVDQU64 (R15), Z16",
        "\tVMOVDQU64 (R14), Z17",
        "\tVPXORD Z27, Z27, Z27",
        "\tVPBROADCASTD 0(SP), Z29",
        "\tVPDPBUSD Z16, Z29, Z27",
        "\tVPXORD Z28, Z28, Z28",
        "\tVPBROADCASTD 0(SP), Z29",
        "\tVPDPBUSD Z17, Z29, Z28",
        "\tVMOVDQU64 (SI), Z18",
        "\tVBROADCASTSS (AX), Y31",
        "\tVBROADCASTSS 4(AX), Y30",
        "\tVINSERTF32X8 $1, Y30, Z31, Z31",
        "\tVBROADCASTSS (BX), Y26",
        "\tVBROADCASTSS 4(BX), Y30",
        "\tVINSERTF32X8 $1, Y30, Z26, Z26",
        # row's prearranged sA lane vec for this tick → Z30 (one 64B load)
        "\tVMOVDQU64 (R8), Z30",
        # B0: pxor + vpdp + psubd(comp) + cvt + mulps(sA) + fma(sB0)
        "\tVPXORD Z29, Z29, Z29",
        "\tVPDPBUSD Z16, Z18, Z29",
        "\tVPSUBD Z27, Z29, Z29",
        "\tVCVTDQ2PS Z29, Z29",
        "\tVMULPS Z30, Z29, Z29",
        "\tVFMADD231PS Z31, Z29, Z0",
        # B1
        "\tVPXORD Z29, Z29, Z29",
        "\tVPDPBUSD Z17, Z18, Z29",
        "\tVPSUBD Z28, Z29, Z29",
        "\tVCVTDQ2PS Z29, Z29",
        "\tVMULPS Z30, Z29, Z29",
        "\tVFMADD231PS Z26, Z29, Z1",
        "\tADDQ $64, R15",
        "\tADDQ $64, R14",
        "\tADDQ $8, AX",
        "\tADDQ $8, BX",
        "\tADDQ $512, SI",
        "\tADDQ $512, R8",
        "\tDECQ R10",
        f"\tJNZ  {label}",
    ]
    text = """// func q8zRowDualVNNIK512(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, off0, off1 int, bn0, bn1 float32, relu bool)
// Q8Z 1-row × 2-B VNNI tail kernel (signed A, +128 comp). Writes out[0..1].
// Frame: out+0 aQ+8 aS+16 qB+24 sB0+32 sB1+40 off0+48 off1+56 bn0+64 bn1+68 relu+72 → 73
// Stack $16: 0(SP)=0x80 const. Registers: Z0/Z1 accums, Z16/17 B, Z18 A,
// Z26 sB1-vec, Z27/28 comp, Z29 temp, Z30 sA-vec, Z31 sB0-vec.
TEXT ·q8zRowDualVNNIK512(SB), NOSPLIT, $16-73
\tMOVQ out+0(FP), R11

\tMOVL $0x80808080, CX
\tMOVL CX, 0(SP)

\tMOVQ aQ+8(FP), SI
\tMOVQ aS+16(FP), R8
\tMOVQ qB+24(FP), R15
\tMOVQ qB+24(FP), R14
\tADDQ off0+48(FP), R15
\tADDQ off1+56(FP), R14
\tMOVQ sB0+32(FP), AX
\tMOVQ sB1+40(FP), BX

\tVXORPS Z0, Z0, Z0
\tVXORPS Z1, Z1, Z1
\tMOVQ $8, R10
"""
    text += "\n".join(lines) + "\n"
    # epilogue: hsum Z0 + bn0 (+relu) → store (R11); same for Z1 at 4(R11).
    text += hsum_z_to_x28(0) + "\n"
    text += "\tVADDSS bn0+64(FP), X28, X28\n"
    text += "\tMOVB relu+72(FP), CL\n\tTESTB CL, CL\n\tJZ q8z_row_norelu0\n"
    text += "\tVXORPS X30, X30, X30\n\tVMAXSS X30, X28, X28\nq8z_row_norelu0:\n"
    text += "\tVMOVSS X28, (R11)\n"
    text += hsum_z_to_x28(1) + "\n"
    text += "\tVADDSS bn1+68(FP), X28, X28\n"
    text += "\tMOVB relu+72(FP), CL\n\tTESTB CL, CL\n\tJZ q8z_row_norelu1\n"
    text += "\tVXORPS X30, X30, X30\n\tVMAXSS X30, X28, X28\nq8z_row_norelu1:\n"
    text += "\tVMOVSS X28, 4(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8z_dual8_accum_vnni_k2048_n512() -> str:
    """Q8Z ZMM per-block accum kernel for FFN down (N=512 K=2048, ReLU A u8).

    32 two-block ticks; per (r,j): pxor + vpdp + cvt + mulps(sA-vec) +
    fma(sB-vec) — no compensation (A is unsigned 0..127). Epilogue is the
    FFN-down residual accumulate: out += hsum + bias.
    """
    text = """// func q8zDual8AccumVNNIK2048N512(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// Q8Z ZMM per-block VNNI, residual accum. N=512 K=2048. A u8 0..127 (no comp).
// Frame: out+0 aQ+8 aS+16 qB+24 sB0+32 sB1+40 off0+48 off1+56 m+64 n+72 bn0+80 bn1+84 → 88
TEXT ·q8zDual8AccumVNNIK2048N512(SB), NOSPLIT, $0-88
\tMOVQ out+0(FP), R11
\tMOVQ m+64(FP), AX
\tMOVQ n+72(FP), BX
\tSHLQ $9, AX // m*512
\tADDQ BX, AX
\tSHLQ $2, AX
\tADDQ AX, R11

\tMOVQ aQ+8(FP), SI
\tMOVQ aS+16(FP), R8
\tMOVQ qB+24(FP), R15
\tMOVQ qB+24(FP), R14
\tADDQ off0+48(FP), R15
\tADDQ off1+56(FP), R14
\tMOVQ sB0+32(FP), AX
\tMOVQ sB1+40(FP), BX

"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += "\tMOVQ $32, R10\nq8z_acc2048_loop:\n"
    text += _q8z_accum_tick() + "\n"
    text += "\tDECQ R10\n\tJNZ  q8z_acc2048_loop\n"
    for r in range(8):
        for j in range(2):
            y = j * 8 + r
            off = r * 2048 + j * 4
            bn = "bn0+80(FP)" if j == 0 else "bn1+84(FP)"
            text += hsum_z_to_x28(y) + "\n"
            text += f"\tVADDSS {bn}, X28, X28\n"
            text += f"\tVADDSS {off}(R11), X28, X28\n"
            text += f"\tVMOVSS X28, {off}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_q8r_dual8_logits_vnni_k512() -> str:
    """Q8R CTC stage-1: 8A×2B int-accumulate → 16 approx logits to vals.

    Same loop/epilogue math as the Q8R GEMM kernels (per-row A scale, sum128
    compensation), but stores [row r][col j] at vals[r*8+j*4] for the Go
    candidate-pruning stage 2. Precision here only affects the candidate set,
    never the final (exact) argmax.
    """
    text = """// func q8rDual8LogitsVNNIK512(vals *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1 *float32, sumB0, sumB1 *float32, off0, off1, m, n int, bn0, bn1 float32)
// Q8R CTC stage 1: per-row-scale int-accumulate VNNI → 16 approx logits.
// vals layout: [row r][col j] at vals[r*8 + j*4] (r=0..7, j=0..1). K=512.
// Frame: vals+0 aQ+8 aS+16 qB+24 sB0+32 sB1+40 sumB0+48 sumB1+56 off0+64 off1+72 m+80 n+88 bn0+96 bn1+100 → 104
TEXT ·q8rDual8LogitsVNNIK512(SB), NOSPLIT, $0-104
\tMOVQ vals+0(FP), R11
\tMOVQ aQ+8(FP), SI
\tMOVQ aS+16(FP), R8
\tMOVQ qB+24(FP), R15
\tMOVQ qB+24(FP), R14
\tADDQ off0+64(FP), R15
\tADDQ off1+72(FP), R14
\tMOVQ sumB0+48(FP), R12
\tMOVQ sumB1+56(FP), R13
\tMOVQ sB0+32(FP), AX
\tMOVQ sB1+40(FP), BX

"""
    for i in range(16):
        text += f"\tVPXORD Y{i}, Y{i}, Y{i}\n"
    text += "\tMOVQ $16, R10\n"
    text += _q8r_dual8_intaccum_loop("q8r_lg_loop") + "\n"
    for r in range(8):
        text += _q8r_epilogue_hscale_store(r, 0, r*8, 0, False, 0) + "\n"
        text += _q8r_epilogue_hscale_store(r, 1, r*8+4, 0, False, 0) + "\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def gen_quantize_panel4_q8s_k512_zmm() -> str:
    """quantizePanel4Q8SK512ZmmAVX512: 4 A rows K=512, per-block scale, ZMM layout.

    4-row variant of quantizePanel8Q8SK512ZmmAVX512 for the 4x4-tile kernels:
      q[g*256 + r*64 : +32] = block 2g of row r; q[+32 : +64] = block 2g+1
      s[b*4 + r] block-major; sA[g*64 + r*16] = [s(2g,r)×8 | s(2g+1,r)×8].
    """

    def amax_chain(src: str, tmpy: str, tmpx: str, dst: str) -> list:
        return [
            f"\tVEXTRACTF32X8 $1, {src}, {tmpy}",
            f"\tVMAXPS Y{src[1:]}, {tmpy}, Y{src[1:]}",
            f"\tVEXTRACTF32X4 $1, Y{src[1:]}, X{tmpx}",
            f"\tVMAXPS X{src[1:]}, X{tmpx}, X{src[1:]}",
            f"\tVSHUFPD $1, X{src[1:]}, X{src[1:]}, X{tmpx}",
            f"\tVMAXPS X{src[1:]}, X{tmpx}, X{src[1:]}",
            f"\tVMOVSHDUP X{src[1:]}, X{tmpx}",
            f"\tVMAXSS X{src[1:]}, X{tmpx}, X{dst}",
        ]

    def dual_block(a_off: int, q_off: int, s_off: int) -> str:
        am0 = "(R11)" if a_off == 0 else f"{a_off}(R11)"
        am1 = f"{a_off + 64}(R11)"
        bm0 = "(R15)" if a_off == 0 else f"{a_off}(R15)"
        bm1 = f"{a_off + 64}(R15)"
        qm0 = "(R12)" if q_off == 0 else f"{q_off}(R12)"
        qm0b = f"{q_off + 16}(R12)"
        qm1 = f"{q_off + 64}(R12)"
        qm1b = f"{q_off + 80}(R12)"
        sm0 = "(R13)" if s_off == 0 else f"{s_off}(R13)"
        sm1 = f"{s_off + 4}(R13)"
        lines = [
            f"\tVMOVUPS {am0}, Z0",
            f"\tVMOVUPS {am1}, Z1",
            f"\tVMOVUPS {bm0}, Z8",
            f"\tVMOVUPS {bm1}, Z9",
            "\tVANDPS Z12, Z0, Z2",
            "\tVANDPS Z12, Z1, Z3",
            "\tVANDPS Z12, Z8, Z10",
            "\tVANDPS Z12, Z9, Z11",
            "\tVMAXPS Z2, Z3, Z2",
            "\tVMAXPS Z10, Z11, Z10",
        ]
        lines += amax_chain("Z2", "Y3", "3", "2")
        lines += amax_chain("Z10", "Y11", "11", "10")
        lines += [
            "\tVMULSS X13, X2, X4",
            "\tVMAXSS X15, X2, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            f"\tVMOVSS X4, {sm0}",
            "\tVMULPS Z5, Z0, Z0",
            "\tVMULPS Z5, Z1, Z1",
            "\tVADDPS Z6, Z0, Z0",
            "\tVADDPS Z6, Z1, Z1",
            "\tVCVTPS2DQ Z0, Z0",
            "\tVCVTPS2DQ Z1, Z1",
            "\tVPMOVDB Z0, X4",
            "\tVPMOVDB Z1, X7",
            f"\tVMOVUPS X4, {qm0}",
            f"\tVMOVUPS X7, {qm0b}",
            "\tVMULSS X13, X10, X4",
            "\tVMAXSS X15, X10, X3",
            "\tVRCP14SS X3, X3, X3",
            "\tVMULSS X14, X3, X3",
            "\tVBROADCASTSS X3, Z5",
            f"\tVMOVSS X4, {sm1}",
            "\tVMULPS Z5, Z8, Z8",
            "\tVMULPS Z5, Z9, Z9",
            "\tVADDPS Z6, Z8, Z8",
            "\tVADDPS Z6, Z9, Z9",
            "\tVCVTPS2DQ Z8, Z8",
            "\tVCVTPS2DQ Z9, Z9",
            "\tVPMOVDB Z8, X4",
            "\tVPMOVDB Z9, X7",
            f"\tVMOVUPS X4, {qm1}",
            f"\tVMOVUPS X7, {qm1b}",
        ]
        return "\n".join(lines)

    text = """// func quantizePanel4Q8SK512ZmmAVX512(q *int8, s *float32, sA *float32, a *float32)
// 4×512 signed A → +128-biased u8 packs [g][4][64] + block-major f32 scales
// + prearranged per-tick lane vecs sA[g*64 + r*16].
// Frame: q+0 s+8 sA+16 a+24 → 32
TEXT ·quantizePanel4Q8SK512ZmmAVX512(SB), NOSPLIT, $32-32
\tMOVQ q+0(FP), DI
\tMOVQ s+8(FP), R8
\tMOVQ a+24(FP), SI
\tMOVL $0x42fe0000, AX // 127.0f
\tMOVL AX, 0(SP)
\tVBROADCASTSS 0(SP), Z14
\tMOVL $0x34000000, AX // ~1.2e-7f eps
\tMOVL AX, 4(SP)
\tVMOVSS 4(SP), X15
\tMOVL $0x3c010204, AX // 1/127.0f
\tMOVL AX, 8(SP)
\tVMOVSS 8(SP), X13
\tMOVL $0x7fffffff, AX // abs mask
\tMOVL AX, 12(SP)
\tVBROADCASTSS 12(SP), Z12
\tMOVL $0x43000000, AX // 128.0f bias
\tMOVL AX, 16(SP)
\tVBROADCASTSS 16(SP), Z6

\tMOVQ $2, R9
\tMOVQ $0, R14 // pair index 0..1 → row = pair*2
q8s4_pair_loop:
\tMOVQ $8, R10 // 8 groups
\tMOVQ R14, AX
\tSHLQ $1, AX // r = pair*2
\tMOVQ AX, BX
\tSHLQ $11, BX // r*2048
\tLEAQ (SI)(BX*1), R11
\tLEAQ 2048(R11), R15
\tMOVQ AX, BX
\tSHLQ $6, BX // r*64
\tLEAQ (DI)(BX*1), R12
\tLEAQ (R8)(AX*4), R13
q8s4_blk_loop:
\tPREFETCHT0 256(R11)
\tPREFETCHT0 256(R15)
"""
    text += dual_block(0, 0, 0) + "\n"
    text += dual_block(128, 32, 16) + "\n"
    text += """\tADDQ $256, R11 // A: 64 floats
\tADDQ $256, R15
\tADDQ $256, R12 // q: 1 group tile (4 rows × 64B)
\tADDQ $32, R13 // s: 2 blocks × 4 floats (32B)
\tDECQ R10
\tJNZ  q8s4_blk_loop
\tINCQ R14
\tDECQ R9
\tJNZ  q8s4_pair_loop

\t// Prearrange per-tick sA lane vecs: sA[g*256 + r*64] = [s(2g,r)×8 | s(2g+1,r)×8].
\tMOVQ sA+16(FP), R9
\tMOVQ $0, R10 // g
q8s4_vec_g:
\tMOVQ $0, R11 // r
q8s4_vec_r:
\tMOVQ R10, AX
\tSHLQ $3, AX // g*8 (4 rows per block)
\tADDQ R11, AX // g*8 + r  (s[(2g)*4 + r])
\tVBROADCASTSS (R8)(AX*4), Y0
\tVMOVUPS Y0, (R9)
\tVBROADCASTSS 16(R8)(AX*4), Y0
\tVMOVUPS Y0, 32(R9)
\tADDQ $64, R9
\tINCQ R11
\tCMPQ R11, $4
\tJB   q8s4_vec_r
\tINCQ R10
\tCMPQ R10, $8
\tJB   q8s4_vec_g
\tVZEROUPPER
\tRET
"""
    return text


def _q8z4x4_tick() -> str:
    """One 2-block tick of the 4-row × 4-col Q8Z kernel.

    Registers: Z0-15 accums (Z[c*4+r]), Z16-19 B0-B3, Z20-23 A rows,
    Z24-27 comp0-3, Z28 0x80 const, Z29 int temp, Z30 sA-vec, Z31 sB-vec.
    sB vecs are prebuilt per call into the stack buffer (R20 advances 64/tick;
    col c at offset c*512). c-outer: sB_c loaded once per c (Z31), sA_r per
    (r,c). Per (r,c): pxor+vpdp+psubd+cvt+mulps+fma — the same per-block-scale
    sequence as the dual8 kernel, so accum lanes are bit-identical.
    """
    lines = [
        "\tPREFETCHT0 256(R15)",
        "\tPREFETCHT0 256(R14)",
        "\tPREFETCHT0 256(R13)",
        "\tPREFETCHT0 256(R12)",
        "\tPREFETCHT0 512(SI)",
        "\tVMOVDQU64 (R15), Z16",
        "\tVMOVDQU64 (R14), Z17",
        "\tVMOVDQU64 (R13), Z18",
        "\tVMOVDQU64 (R12), Z19",
    ]
    for i in range(4):
        lines.append(f"\tVMOVDQU64 {i * 64}(SI), Z{20 + i}")
    for c in range(4):
        lines += [
            f"\tVPXORD Z{24 + c}, Z{24 + c}, Z{24 + c}",
            f"\tVPDPBUSD Z{16 + c}, Z28, Z{24 + c}",
        ]
    for c, sreg in enumerate(["AX", "BX", "CX", "DX"]):
        lines += [
            f"\tVBROADCASTSS ({sreg}), Y31",
            f"\tVBROADCASTSS 4({sreg}), Y30",
            "\tVINSERTF32X8 $1, Y30, Z31, Z31",
        ]
        for r in range(4):
            lines += [
                f"\tVMOVDQU64 {r * 64}(R8), Z30",  # sA_r vec
                "\tVPXORD Z29, Z29, Z29",
                f"\tVPDPBUSD Z{16 + c}, Z{20 + r}, Z29",
                f"\tVPSUBD Z{24 + c}, Z29, Z29",
                "\tVCVTDQ2PS Z29, Z29",
                "\tVMULPS Z30, Z29, Z29",
                f"\tVFMADD231PS Z31, Z29, Z{c * 4 + r}",
            ]
    lines += [
        "\tADDQ $64, R15",
        "\tADDQ $64, R14",
        "\tADDQ $64, R13",
        "\tADDQ $64, R12",
        "\tADDQ $8, AX",
        "\tADDQ $8, BX",
        "\tADDQ $8, CX",
        "\tADDQ $8, DX",
        "\tADDQ $256, SI",
        "\tADDQ $256, R8",
    ]
    return "\n".join(lines)


def _q8z4x4_prologue() -> str:
    """Pointer setup + per-call sB-vec build into the stack buffer.

    AX..DX hold the 4 cols' scale pointers (advance 8B/tick) for the inline
    per-tick sB-vec builds (Z31).
    """
    text = """\tMOVQ aQ+8(FP), SI
\tMOVQ aS+16(FP), R8
\tMOVQ qB+24(FP), R15
\tMOVQ qB+24(FP), R14
\tMOVQ qB+24(FP), R13
\tMOVQ qB+24(FP), R12
\tADDQ off0+64(FP), R15
\tADDQ off1+72(FP), R14
\tADDQ off2+80(FP), R13
\tADDQ off3+88(FP), R12
\tMOVQ sB0+32(FP), AX
\tMOVQ sB1+40(FP), BX
\tMOVQ sB2+48(FP), CX
\tMOVQ sB3+56(FP), DX

\tMOVL $0x80808080, R9
\tMOVL R9, 0(SP)
\tVPBROADCASTD 0(SP), Z28

"""
    for i in range(16):
        text += f"\tVXORPS Z{i}, Z{i}, Z{i}\n"
    text += "\tMOVQ $8, R10\n"
    return text


def gen_q8z_dual4x4_vnni_k512(n_cols: int) -> str:
    """Q8Z 4-row × 4-col ZMM per-block kernel for N=512/1536/2048."""
    if n_cols == 512:
        row_calc = "\tSHLQ $9, AX // m*512\n"
        row_bytes = 2048
    elif n_cols == 1536:
        row_calc = "\tLEAQ (AX)(AX*2), AX // m*3\n\tSHLQ $9, AX // m*1536\n"
        row_bytes = 6144
    elif n_cols == 2048:
        row_calc = "\tSHLQ $11, AX // m*2048\n"
        row_bytes = 8192
    else:
        raise ValueError(f"unsupported N={n_cols}")
    text = f"""// func q8zDual4x4VNNIK512N{n_cols}(out *float32, aQ *int8, aS *float32, qB *byte, sB0, sB1, sB2, sB3 *float32, off0, off1, off2, off3, m, n int, bn0, bn1, bn2, bn3 float32)
// Q8Z 4×4-tile ZMM per-block VNNI: A_u8(+128) × B_s8 stripped. N={n_cols} K=512.
// Writes rows m..m+3, cols n..n+3. Bit-identical accum lanes to q8zDual8*.
// Frame: out+0 aQ+8 aS+16 qB+24 sB0+32 sB1+40 sB2+48 sB3+56 off0+64 off1+72 off2+80 off3+88 m+96 n+104 bn0+112 bn1+116 bn2+120 bn3+124 → 128
// Stack $8: 0(SP)=0x80 const. inline per-tick sB builds (AX-DX scale ptrs).
// Registers: Z0-15 accums (Z[c*4+r]), Z16-19 B, Z20-23 A, Z24-27 comp,
// Z28 0x80 const, Z29 temp, Z30 sA-vec, Z31 sB-vec.
TEXT ·q8zDual4x4VNNIK512N{n_cols}(SB), NOSPLIT, $8-128
\tMOVQ out+0(FP), R11
\tMOVQ m+96(FP), AX
\tMOVQ n+104(FP), BX
{row_calc}\tADDQ BX, AX
\tSHLQ $2, AX
\tADDQ AX, R11

"""
    text += _q8z4x4_prologue()
    text += "q8z4x_pl" + str(n_cols) + "_loop:\n" + _q8z4x4_tick() + "\n"
    text += "\tDECQ R10\n\tJNZ  q8z4x_pl" + str(n_cols) + "_loop\n"
    for r in range(4):
        for c in range(4):
            y = c * 4 + r
            off = r * row_bytes + c * 4
            bn = f"bn{c}+{112 + c * 4}(FP)"
            text += hsum_z_to_x28(y) + "\n"
            text += f"\tVADDSS {bn}, X28, X28\n"
            if n_cols == 2048:
                text += "\tVXORPS X30, X30, X30\n\tVMAXSS X30, X28, X28\n"
            text += f"\tVMOVSS X28, {off}(R11)\n"
    text += "\tVZEROUPPER\n\tRET\n"
    return text


def main() -> None:
    out = Path("corelib/embedding/tensor/avx512_kernels_amd64.s")
    parts = [
        "//go:build amd64\n\n#include \"textflag.h\"\n\n",
        gen_multidot4_generic16(),
        "\n",
        "// AUTO-generated by scripts/gen_avx512_kernels.py — SenseVoice AVX-512 hot kernels.\n\n",
        gen_multidot8_triple_k512(),
        "\n",
        gen_multidot8_triple_relu_k512_n2048(),
        "\n",
        gen_multidot8_triple_argmax_k512(),
        "\n",
        gen_multidot8_triple_plain_k512(512),
        "\n",
        gen_multidot8_triple_plain_k512(1536),
        "\n",
        gen_multidot4_triple_k512(),
        "\n",
        gen_multidot4_dual_k512(),
        "\n",
        gen_q8_dual_n64(),
        "\n",
        gen_q8_dual8_n64(),
        "\n",
        gen_q8_dual8_accum_n64(),
        "\n",
        gen_quantize_panel8_q8u_k2048(),
        "\n",
        gen_q8u_q8s_dual8_accum_vnni(),
        "\n",
        gen_quantize_panel8_q8s_k512(),
        "\n",
        gen_q8u_q8s_dual8_accum_vnni_k512(),
        "\n",
        gen_q8u_q8s_dual8_plain_vnni_k512_n1536(),
        "\n",
        gen_q8u_q8s_dual8_relu_vnni_k512_n2048(),
        "\n",
        gen_q8u_q8s_dual8_argmax_vnni_k512(),
        "\n",
        gen_q8u_q8s_dual8_logits_vnni_k512(),
        "\n",
        gen_q8r_dual8_logits_vnni_k512(),
        "\n",
        gen_quantize_panel8_q8srow_k512(),
        "\n",
        gen_q8r_dual8_plain_vnni_k512(512),
        "\n",
        gen_q8r_dual8_plain_vnni_k512(1536),
        "\n",
        gen_q8r_dual8_relu_vnni_k512_n2048(),
        "\n",
        gen_quantize_panel8_q8s_k512_zmm(),
        "\n",
        gen_q8z_dual8_plain_vnni_k512(512),
        "\n",
        gen_q8z_dual8_plain_vnni_k512(1536),
        "\n",
        gen_q8z_dual8_relu_vnni_k512_n2048(),
        "\n",
        gen_quantize_panel4_q8s_k512_zmm(),
        "\n",
        gen_q8z_dual4x4_vnni_k512(512),
        "\n",
        gen_q8z_dual4x4_vnni_k512(1536),
        "\n",
        gen_q8z_dual4x4_vnni_k512(2048),
        "\n",
        gen_q8z_row_dual_vnni_k512(),
        "\n",
        gen_quantize_panel8_q8u_k2048_zmm(),
        "\n",
        gen_q8z_dual8_accum_vnni_k2048_n512(),
        "\n",
        # Quad 8A×4B kept in source for experiments but not emitted: stack-hsum
        # and 4-row×2-pass variants lost to dual vector-accum on Zen4.
        # gen_q8u_q8s_quad8_accum_vnni(),

        gen_multidot8_dual_k512(),
        "\n",
        gen_multidot4_dual_k560(),
        "\n",
        gen_multidot8_dual_k560(),
        "\n",
        gen_multidot8_dual_plain_k512(512),
        "\n",
        gen_multidot8_dual_plain_k512(1536),
        "\n",
        gen_multidot8_dual_argmax_k512(),
        "\n",
        gen_multidot8_dual_relu_k512_n2048(),
        "\n",
        gen_multidot4_dual_k128(),
        "\n",
        gen_multidot4_triple_k128(),
        "\n",
        gen_multidot8_dual_k128(),
        "\n",
        gen_multidot8_triple_k128(),
        "\n",
        gen_multidot4_k512(),
        "\n",
        gen_q8_single_n64(),
        "\n",
        gen_dequant_single(),
        "\n",
        gen_dequant_dual(),
        "\n",
        gen_dequant_triple(),
        "\n",
        gen_dot_q8_scaled(),
        "\n",
        gen_dual_dot2_scaled(),
        "\n",
        gen_wsum8_dual_128(),
    ]
    out.write_text("".join(parts), encoding="utf-8")
    print("wrote", out, "bytes", out.stat().st_size)


if __name__ == "__main__":
    main()

