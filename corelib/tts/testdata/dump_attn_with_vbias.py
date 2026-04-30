#!/usr/bin/env python3
"""Dump attention output WITH value bias for comparison."""
import os, json, math, numpy as np, torch
import torch.nn.functional as F

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
ckpt = torch.load(os.path.join(model_dir, "checkpoint.pth"), map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)

T = 33; hidden = 192; n_heads = 2; head_dim = 96; window_size = 4

h = torch.from_numpy(np.fromfile(os.path.join(outdir, "ref_flow_attn_00_pre.bin"),
    dtype=np.float32).reshape(hidden, T)).unsqueeze(0)

p = "flow.flows.6.enc."

with torch.no_grad():
    qw = sd[f"{p}attn_layers.0.conv_q.weight"]; qb = sd[f"{p}attn_layers.0.conv_q.bias"]
    kw = sd[f"{p}attn_layers.0.conv_k.weight"]; kb = sd[f"{p}attn_layers.0.conv_k.bias"]
    vw = sd[f"{p}attn_layers.0.conv_v.weight"]; vb = sd[f"{p}attn_layers.0.conv_v.bias"]

    q = F.conv1d(h, qw, qb); k = F.conv1d(h, kw, kb); v = F.conv1d(h, vw, vb)

    q_h = q.view(1, n_heads, head_dim, T).transpose(2, 3)
    k_h = k.view(1, n_heads, head_dim, T).transpose(2, 3)
    v_h = v.view(1, n_heads, head_dim, T).transpose(2, 3)

    scores = torch.matmul(q_h / math.sqrt(head_dim), k_h.transpose(-2, -1))

    emb_rel_k = sd[f"{p}attn_layers.0.emb_rel_k"]
    pad_length = max(T - (window_size + 1), 0)
    padded = F.pad(emb_rel_k, [0, 0, pad_length, pad_length])
    used = padded[:, :2*T-1]
    rel_logits = torch.matmul(q_h / math.sqrt(head_dim), used.unsqueeze(0).transpose(-2, -1))
    x_pad = F.pad(rel_logits, [0, 1])
    x_flat = x_pad.view(1, n_heads, T * 2 * T)
    x_flat = F.pad(x_flat, [0, T - 1])
    scores_local = x_flat.view(1, n_heads, T + 1, 2 * T - 1)[:, :, :T, T - 1:]
    scores = scores + scores_local

    p_attn = F.softmax(scores, dim=-1)

    # Output WITHOUT value bias
    output_no_vbias = torch.matmul(p_attn, v_h)
    out_no_vbias = output_no_vbias.transpose(2, 3).contiguous().view(1, hidden, T)
    d = out_no_vbias.squeeze(0).numpy().astype(np.float32)
    d.tofile(os.path.join(outdir, "ref_attn_out_no_vbias.bin"))
    print(f"no_vbias: mean={d.mean():.6f}, std={d.std():.6f}")

    # Output WITH value bias
    emb_rel_v = sd[f"{p}attn_layers.0.emb_rel_v"]
    padded_v = F.pad(emb_rel_v, [0, 0, pad_length, pad_length])
    used_v = padded_v[:, :2*T-1]
    rel_w = F.pad(p_attn, [0, T - 1])
    rel_flat = rel_w.view(1, n_heads, T**2 + T*(T-1))
    rel_flat = F.pad(rel_flat, [T, 0])
    rel_final = rel_flat.view(1, n_heads, T, 2*T)[:, :, :, 1:]
    output_with_vbias = output_no_vbias + torch.matmul(rel_final, used_v.unsqueeze(0))
    out_with_vbias = output_with_vbias.transpose(2, 3).contiguous().view(1, hidden, T)
    d2 = out_with_vbias.squeeze(0).numpy().astype(np.float32)
    d2.tofile(os.path.join(outdir, "ref_attn_out_with_vbias.bin"))
    print(f"with_vbias: mean={d2.mean():.6f}, std={d2.std():.6f}")

    # Diff
    diff = np.abs(d - d2)
    print(f"vbias contribution: maxDiff={diff.max():.6f}, avgDiff={diff.mean():.6f}")
