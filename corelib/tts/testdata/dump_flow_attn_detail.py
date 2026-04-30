#!/usr/bin/env python3
"""Dump flow attention layer details for debugging relative position bias."""
import os, json, math, numpy as np, torch
import torch.nn.functional as F

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")

ckpt = torch.load(os.path.join(model_dir, "checkpoint.pth"), map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)

# Load the z_p and g from official flow dump
z_p = torch.from_numpy(np.fromfile(os.path.join(outdir, "ref_official_flow_z_p.bin"),
    dtype=np.float32).reshape(192, 33)).unsqueeze(0)
g = torch.from_numpy(np.fromfile(os.path.join(outdir, "ref_official_flow_g.bin"),
    dtype=np.float32).reshape(256, 1)).unsqueeze(0)

T_mel = 33
hidden = 192
n_heads = 2
head_dim = 96
window_size = 4

def dump(name, t):
    d = t.detach().cpu().numpy().astype(np.float32)
    while d.ndim > 1 and d.shape[0] == 1:
        d = d[0]
    d.tofile(os.path.join(outdir, f"{name}.bin"))
    flat = d.flatten()
    print(f"  {name}: shape={list(d.shape)}, mean={flat.mean():.6f}, std={flat.std():.6f}")

with torch.no_grad():
    # Flow reverse: last coupling layer first (index 3, flows[6])
    z = z_p.clone()

    # Flip
    z = torch.flip(z, [1])

    # Coupling layer 3 (flows[6])
    half = 96
    x0 = z[:, :half, :]
    x1 = z[:, half:, :]

    # Pre projection
    pre_w = sd["flow.flows.6.pre.weight"]
    pre_b = sd["flow.flows.6.pre.bias"]
    h = F.conv1d(x0, pre_w, pre_b)
    dump("ref_flow_attn_00_pre", h)

    # First FFT layer (layer 0 of coupling 3)
    p = "flow.flows.6.enc."

    # Attention on raw h (post-norm architecture)
    qw = sd[f"{p}attn_layers.0.conv_q.weight"]
    qb = sd[f"{p}attn_layers.0.conv_q.bias"]
    kw = sd[f"{p}attn_layers.0.conv_k.weight"]
    kb = sd[f"{p}attn_layers.0.conv_k.bias"]
    vw = sd[f"{p}attn_layers.0.conv_v.weight"]
    vb = sd[f"{p}attn_layers.0.conv_v.bias"]

    q = F.conv1d(h, qw, qb)
    k = F.conv1d(h, kw, kb)
    v = F.conv1d(h, vw, vb)
    dump("ref_flow_attn_01_q", q)
    dump("ref_flow_attn_01_k", k)

    # Reshape for multi-head attention
    b = 1
    q_h = q.view(b, n_heads, head_dim, T_mel).transpose(2, 3)  # [1, 2, 33, 96]
    k_h = k.view(b, n_heads, head_dim, T_mel).transpose(2, 3)
    v_h = v.view(b, n_heads, head_dim, T_mel).transpose(2, 3)

    # Standard attention scores
    scores = torch.matmul(q_h / math.sqrt(head_dim), k_h.transpose(-2, -1))  # [1, 2, 33, 33]
    dump("ref_flow_attn_02_scores_before_rel", scores)

    # Relative position bias
    emb_rel_k = sd[f"{p}attn_layers.0.emb_rel_k"]  # [1, 9, 96]
    print(f"  emb_rel_k shape: {list(emb_rel_k.shape)}")

    # _get_relative_embeddings
    pad_length = max(T_mel - (window_size + 1), 0)  # 33 - 5 = 28
    slice_start = max((window_size + 1) - T_mel, 0)  # 0
    slice_end = slice_start + 2 * T_mel - 1  # 65

    if pad_length > 0:
        padded = F.pad(emb_rel_k, [0, 0, pad_length, pad_length])  # [1, 9+56, 96] = [1, 65, 96]
    else:
        padded = emb_rel_k
    used = padded[:, slice_start:slice_end]  # [1, 65, 96]
    print(f"  padded shape: {list(padded.shape)}, used shape: {list(used.shape)}")
    dump("ref_flow_attn_03_rel_emb_used", used)

    # Q @ rel_K^T
    rel_logits = torch.matmul(q_h / math.sqrt(head_dim), used.unsqueeze(0).transpose(-2, -1))
    # [1, 2, 33, 65]
    dump("ref_flow_attn_04_rel_logits", rel_logits)

    # _relative_position_to_absolute_position
    length = T_mel
    # Pad: [1, 2, 33, 65] → [1, 2, 33, 66]
    x_pad = F.pad(rel_logits, [0, 1])
    # Flatten: [1, 2, 33*66] = [1, 2, 2178]
    x_flat = x_pad.view(b, n_heads, length * 2 * length)
    # Pad: [1, 2, 2178+32] = [1, 2, 2210]
    x_flat = F.pad(x_flat, [0, length - 1])
    # Reshape: [1, 2, 34, 65] → slice [1, 2, 33, 33]
    x_final = x_flat.view(b, n_heads, length + 1, 2 * length - 1)[:, :, :length, length - 1:]
    dump("ref_flow_attn_05_scores_local", x_final)

    # Combined scores
    scores_combined = scores + x_final
    dump("ref_flow_attn_06_scores_combined", scores_combined)

    # Softmax
    p_attn = F.softmax(scores_combined, dim=-1)
    dump("ref_flow_attn_07_p_attn", p_attn)

    # Value output
    output = torch.matmul(p_attn, v_h)  # [1, 2, 33, 96]

    # Relative value bias
    emb_rel_v = sd[f"{p}attn_layers.0.emb_rel_v"]
    if pad_length > 0:
        padded_v = F.pad(emb_rel_v, [0, 0, pad_length, pad_length])
    else:
        padded_v = emb_rel_v
    used_v = padded_v[:, slice_start:slice_end]

    # _absolute_position_to_relative_position
    rel_weights = F.pad(p_attn, [0, length - 1])
    rel_flat = rel_weights.view(b, n_heads, length**2 + length * (length - 1))
    rel_flat = F.pad(rel_flat, [length, 0])
    rel_final = rel_flat.view(b, n_heads, length, 2 * length)[:, :, :, 1:]

    output = output + torch.matmul(rel_final, used_v.unsqueeze(0))

    output = output.transpose(2, 3).contiguous().view(b, hidden, T_mel)
    dump("ref_flow_attn_08_attn_output", output)

print("\nDone!")
