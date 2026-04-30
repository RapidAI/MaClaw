#!/usr/bin/env python3
"""Dump one full encoder layer output from flow for Go comparison."""
import os, json, math, numpy as np, torch
import torch.nn.functional as F

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
ckpt = torch.load(os.path.join(model_dir, "checkpoint.pth"), map_location="cpu", weights_only=False)
sd = ckpt.get("model", ckpt)

T = 33; hidden = 192; n_heads = 2; head_dim = 96; window_size = 4

def dump(name, t):
    d = t.detach().cpu().numpy().astype(np.float32)
    while d.ndim > 1 and d.shape[0] == 1: d = d[0]
    d.tofile(os.path.join(outdir, f"{name}.bin"))
    print(f"  {name}: shape={list(d.shape)}, mean={d.mean():.6f}, std={d.std():.6f}")

h = torch.from_numpy(np.fromfile(os.path.join(outdir, "ref_flow_attn_00_pre.bin"),
    dtype=np.float32).reshape(hidden, T)).unsqueeze(0)

p = "flow.flows.6.enc."

with torch.no_grad():
    # Full attention (using PyTorch's MultiHeadAttention equivalent)
    qw = sd[f"{p}attn_layers.0.conv_q.weight"]; qb = sd[f"{p}attn_layers.0.conv_q.bias"]
    kw = sd[f"{p}attn_layers.0.conv_k.weight"]; kb = sd[f"{p}attn_layers.0.conv_k.bias"]
    vw = sd[f"{p}attn_layers.0.conv_v.weight"]; vb = sd[f"{p}attn_layers.0.conv_v.bias"]
    ow = sd[f"{p}attn_layers.0.conv_o.weight"]; ob = sd[f"{p}attn_layers.0.conv_o.bias"]

    q = F.conv1d(h, qw, qb); k = F.conv1d(h, kw, kb); v = F.conv1d(h, vw, vb)

    q_h = q.view(1, n_heads, head_dim, T).transpose(2, 3)
    k_h = k.view(1, n_heads, head_dim, T).transpose(2, 3)
    v_h = v.view(1, n_heads, head_dim, T).transpose(2, 3)

    scores = torch.matmul(q_h / math.sqrt(head_dim), k_h.transpose(-2, -1))

    # Relative position bias
    emb_rel_k = sd[f"{p}attn_layers.0.emb_rel_k"]
    pad_length = max(T - (window_size + 1), 0)
    padded = F.pad(emb_rel_k, [0, 0, pad_length, pad_length])
    used = padded[:, :2*T-1]
    rel_logits = torch.matmul(q_h / math.sqrt(head_dim), used.unsqueeze(0).transpose(-2, -1))

    # rel_to_abs
    length = T
    x_pad = F.pad(rel_logits, [0, 1])
    x_flat = x_pad.view(1, n_heads, length * 2 * length)
    x_flat = F.pad(x_flat, [0, length - 1])
    scores_local = x_flat.view(1, n_heads, length + 1, 2 * length - 1)[:, :, :length, length - 1:]

    scores = scores + scores_local
    p_attn = F.softmax(scores, dim=-1)
    output = torch.matmul(p_attn, v_h)

    # Relative value bias
    emb_rel_v = sd[f"{p}attn_layers.0.emb_rel_v"]
    padded_v = F.pad(emb_rel_v, [0, 0, pad_length, pad_length])
    used_v = padded_v[:, :2*T-1]
    rel_w = F.pad(p_attn, [0, length - 1])
    rel_flat = rel_w.view(1, n_heads, length**2 + length*(length-1))
    rel_flat = F.pad(rel_flat, [length, 0])
    rel_final = rel_flat.view(1, n_heads, length, 2*length)[:, :, :, 1:]
    output = output + torch.matmul(rel_final, used_v.unsqueeze(0))

    attn_out = output.transpose(2, 3).contiguous().view(1, hidden, T)
    y = F.conv1d(attn_out, ow, ob)
    dump("ref_enc_layer_01_attn_y", y)

    # Post-norm: h = norm1(h + y)
    h_res = h + y
    # LayerNorm
    n1g = sd[f"{p}norm_layers_1.0.gamma"]
    n1b = sd[f"{p}norm_layers_1.0.beta"]
    h_t = h_res.transpose(1, 2)  # [1, T, C]
    h_normed = F.layer_norm(h_t, (hidden,), n1g, n1b, 1e-5)
    h = h_normed.transpose(1, 2)  # [1, C, T]
    dump("ref_enc_layer_02_after_norm1", h)

    # FFN
    fw1 = sd[f"{p}ffn_layers.0.conv_1.weight"]; fb1 = sd[f"{p}ffn_layers.0.conv_1.bias"]
    fw2 = sd[f"{p}ffn_layers.0.conv_2.weight"]; fb2 = sd[f"{p}ffn_layers.0.conv_2.bias"]
    ffn = F.conv1d(h, fw1, fb1, padding=2)  # ksize=5
    ffn = torch.relu(ffn)
    ffn = F.conv1d(ffn, fw2, fb2, padding=2)
    dump("ref_enc_layer_03_ffn_y", ffn)

    # Post-norm: h = norm2(h + ffn)
    h_res2 = h + ffn
    n2g = sd[f"{p}norm_layers_2.0.gamma"]
    n2b = sd[f"{p}norm_layers_2.0.beta"]
    h_t2 = h_res2.transpose(1, 2)
    h_normed2 = F.layer_norm(h_t2, (hidden,), n2g, n2b, 1e-5)
    h = h_normed2.transpose(1, 2)
    dump("ref_enc_layer_04_output", h)

print("Done!")
