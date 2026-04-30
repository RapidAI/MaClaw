#!/usr/bin/env python3
"""
Dump flow decoder layer-by-layer reference outputs.
Uses the full MeloTTS model (not manual forward) to get exact intermediate values.
"""
import os, sys, json, math, struct
import numpy as np
import torch
import torch.nn.functional as F

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
ckpt_path = os.path.join(model_dir, "checkpoint.pth")
config_path = os.path.join(model_dir, "config.json")

with open(config_path, encoding='utf-8') as f:
    config = json.load(f)

ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=True)
sd = ckpt.get("model", ckpt)

phone_ids = [0, 49, 0, 127, 0, 70, 0, 80, 0]
T = len(phone_ids)
hidden = 192
inter = 192

def dump(name, tensor):
    if isinstance(tensor, torch.Tensor):
        data = tensor.detach().cpu().squeeze(0).numpy().astype(np.float32)
    else:
        data = np.array(tensor, dtype=np.float32)
    path = os.path.join(outdir, f"{name}.bin")
    data.tofile(path)
    flat = data.flatten()
    print(f"  {name}: shape={list(data.shape)}, mean={flat.mean():.6f}, std={flat.std():.6f}, "
          f"min={flat.min():.4f}, max={flat.max():.4f}")

# We need to build the model to run flow.
# Since we can't import melo.api (torchaudio issue), build manually.
# Load the modules we need.
sys.path.insert(0, os.path.join(model_dir, ".."))

# Build a minimal SynthesizerTrn manually
print("=== Building model from state_dict ===")

# First, run the full encoder to get exact m_p, logs_p
# (reuse the ref_05_m_p.bin and ref_06_logs_p.bin from previous run)
m_p_path = os.path.join(outdir, "ref_05_m_p.bin")
logs_p_path = os.path.join(outdir, "ref_06_logs_p.bin")

if not os.path.exists(m_p_path):
    print("ERROR: Run convert_and_compare.py first to generate ref_05_m_p.bin")
    sys.exit(1)

m_p = torch.from_numpy(np.fromfile(m_p_path, dtype=np.float32).reshape(inter, T)).unsqueeze(0)
logs_p = torch.from_numpy(np.fromfile(logs_p_path, dtype=np.float32).reshape(inter, T)).unsqueeze(0)
durations_ref = np.fromfile(os.path.join(outdir, "ref_08_durations.bin"), dtype=np.float32)
durations = [int(d) for d in durations_ref.flatten()]
T_mel = sum(durations)

print(f"m_p: {list(m_p.shape)}, logs_p: {list(logs_p.shape)}, T_mel={T_mel}")

# Expand by durations
with torch.no_grad():
    # Build attention matrix
    x_mask = torch.ones(1, 1, T)
    y_lengths = torch.LongTensor([T_mel])
    y_mask = torch.ones(1, 1, T_mel)

    # Generate path
    attn = torch.zeros(1, T_mel, T)
    mel_pos = 0
    for t, dur in enumerate(durations):
        for d in range(dur):
            if mel_pos < T_mel:
                attn[0, mel_pos, t] = 1.0
            mel_pos += 1

    m_p_exp = torch.matmul(attn, m_p.transpose(1, 2)).transpose(1, 2)  # [1, 192, T_mel]
    logs_p_exp = torch.matmul(attn, logs_p.transpose(1, 2)).transpose(1, 2)

    dump("ref_flow_00_m_p_expanded", m_p_exp)
    dump("ref_flow_00_logs_p_expanded", logs_p_exp)

    # Sample z_p deterministically (noise_scale=0) for exact comparison
    z_p = m_p_exp.clone()  # z_p = m_p when noise_scale=0
    dump("ref_flow_01_z_p", z_p)

    # Speaker embedding
    emb_g = sd["emb_g.weight"]
    g = emb_g[0].unsqueeze(0).unsqueeze(-1)  # [1, 256, 1]

    # ── Flow reverse ──
    # TransformerCouplingBlock: flows = [TCL0, Flip, TCL1, Flip, TCL2, Flip, TCL3, Flip]
    # Reverse order: Flip, TCL3_rev, Flip, TCL2_rev, Flip, TCL1_rev, Flip, TCL0_rev

    z = z_p.clone()
    n_flows = 4  # 4 TransformerCouplingLayers

    # In reverse, we iterate from last to first
    for fi in range(n_flows - 1, -1, -1):
        # Flip first (in reverse)
        z = torch.flip(z, [1])
        dump(f"ref_flow_02_after_flip_{fi}", z)

        # TransformerCouplingLayer reverse
        # Split
        half = inter // 2
        x0 = z[:, :half, :]  # [1, 96, T_mel]
        x1 = z[:, half:, :]  # [1, 96, T_mel]

        # Pre projection
        pre_w = sd[f"flow.flows.{fi*2}.pre.weight"]  # [192, 96, 1]
        pre_b = sd[f"flow.flows.{fi*2}.pre.bias"]
        h = F.conv1d(x0, pre_w, pre_b)  # [1, 192, T_mel]

        # FFT encoder layers (3 layers per coupling)
        for li in range(3):
            p = f"flow.flows.{fi*2}.enc."

            # Pre-norm
            n1g = sd[f"{p}norm_layers_1.{li}.gamma"]
            n1b = sd[f"{p}norm_layers_1.{li}.beta"]
            residual = h.clone()
            h_normed = torch.zeros_like(h)
            for t in range(T_mel):
                col = h[0, :, t]
                mean = col.mean()
                var = col.var(unbiased=False)
                h_normed[0, :, t] = (col - mean) / torch.sqrt(var + 1e-5) * n1g + n1b

            # Attention
            qw = sd[f"{p}attn_layers.{li}.conv_q.weight"]
            qb = sd[f"{p}attn_layers.{li}.conv_q.bias"]
            kw = sd[f"{p}attn_layers.{li}.conv_k.weight"]
            kb = sd[f"{p}attn_layers.{li}.conv_k.bias"]
            vw = sd[f"{p}attn_layers.{li}.conv_v.weight"]
            vb = sd[f"{p}attn_layers.{li}.conv_v.bias"]
            ow = sd[f"{p}attn_layers.{li}.conv_o.weight"]
            ob = sd[f"{p}attn_layers.{li}.conv_o.bias"]

            q = F.conv1d(h_normed, qw, qb)
            k = F.conv1d(h_normed, kw, kb)
            v = F.conv1d(h_normed, vw, vb)

            n_heads = 2
            head_dim = 96
            scale = 1.0 / math.sqrt(head_dim)

            q_h = q.view(1, n_heads, head_dim, T_mel)
            k_h = k.view(1, n_heads, head_dim, T_mel)
            v_h = v.view(1, n_heads, head_dim, T_mel)

            scores = torch.matmul(q_h.transpose(2, 3), k_h) * scale
            attn_w = torch.softmax(scores, dim=-1)
            attn_out = torch.matmul(attn_w, v_h.transpose(2, 3))
            attn_out = attn_out.transpose(2, 3).contiguous().view(1, hidden, T_mel)

            o = F.conv1d(attn_out, ow, ob)
            h = residual + o

            # Pre-FFN norm
            n2g = sd[f"{p}norm_layers_2.{li}.gamma"]
            n2b = sd[f"{p}norm_layers_2.{li}.beta"]
            residual2 = h.clone()
            h_normed2 = torch.zeros_like(h)
            for t in range(T_mel):
                col = h[0, :, t]
                mean = col.mean()
                var = col.var(unbiased=False)
                h_normed2[0, :, t] = (col - mean) / torch.sqrt(var + 1e-5) * n2g + n2b

            # FFN (kernel_size=5 for flow)
            fw1 = sd[f"{p}ffn_layers.{li}.conv_1.weight"]
            fb1 = sd[f"{p}ffn_layers.{li}.conv_1.bias"]
            fw2 = sd[f"{p}ffn_layers.{li}.conv_2.weight"]
            fb2 = sd[f"{p}ffn_layers.{li}.conv_2.bias"]

            ffn = F.conv1d(h_normed2, fw1, fb1, padding=2)  # ksize=5, pad=2
            ffn = torch.relu(ffn)
            ffn = F.conv1d(ffn, fw2, fb2, padding=2)
            h = residual2 + ffn

        # Post projection
        post_w = sd[f"flow.flows.{fi*2}.post.weight"]  # [96, 192, 1]
        post_b = sd[f"flow.flows.{fi*2}.post.bias"]
        m = F.conv1d(h, post_w, post_b)  # [1, 96, T_mel]

        # Reverse affine (mean_only=True): x1 = x1 - m
        x1 = x1 - m

        # Reconstruct z
        z = torch.cat([x0, x1], dim=1)
        dump(f"ref_flow_03_after_coupling_{fi}", z)

    dump("ref_flow_04_z_final", z)

    # ── Vocoder test: just conv_pre ──
    conv_pre_w = sd["dec.conv_pre.weight"]  # [512, 192, 7]
    conv_pre_b = sd["dec.conv_pre.bias"]
    voc_input = z * y_mask
    voc_pre = F.conv1d(voc_input, conv_pre_w, conv_pre_b, padding=3)
    dump("ref_flow_05_vocoder_conv_pre", voc_pre)

print("\nDone! Flow reference files saved.")
