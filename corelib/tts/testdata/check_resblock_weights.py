#!/usr/bin/env python3
"""Check resblock weight_norm reconstruction."""
import os, numpy as np, torch

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
ckpt = torch.load(os.path.join(model_dir, "checkpoint.pth"), map_location="cpu", weights_only=True)
sd = ckpt.get("model", ckpt)

# Check resblock 0, convs1[0]
wg = sd["dec.resblocks.0.convs1.0.weight_g"]  # [256, 1, 1]
wv = sd["dec.resblocks.0.convs1.0.weight_v"]  # [256, 256, 3]
b = sd["dec.resblocks.0.convs1.0.bias"]

print(f"wg: {list(wg.shape)}, wv: {list(wv.shape)}, bias: {list(b.shape)}")

# PyTorch weight_norm: weight = g * v / ||v||_2
# g: [outCh, 1, 1], v: [outCh, inCh, kSize]
# norm is per outCh: ||v[oc]||_2 over [inCh, kSize]
v_norm = wv.norm(dim=[1, 2], keepdim=True)  # [256, 1, 1]
w_reconstructed = wg * wv / v_norm

print(f"v_norm: mean={v_norm.mean():.4f}, min={v_norm.min():.4f}, max={v_norm.max():.4f}")
print(f"w_reconstructed: mean={w_reconstructed.mean():.6f}, std={w_reconstructed.std():.6f}")
print(f"  min={w_reconstructed.min():.6f}, max={w_reconstructed.max():.6f}")

# Dump for Go comparison
w_np = w_reconstructed.detach().numpy().astype(np.float32)
w_np.tofile(os.path.join(outdir, "ref_resblock0_convs1_0_weight.bin"))
b.detach().numpy().astype(np.float32).tofile(os.path.join(outdir, "ref_resblock0_convs1_0_bias.bin"))
print(f"\nDumped weight: {list(w_np.shape)}, {w_np.nbytes} bytes")

# Also check ups[0] weight reconstruction
wg_up = sd["dec.ups.0.weight_g"]  # [512, 1, 1]
wv_up = sd["dec.ups.0.weight_v"]  # [512, 256, 16]
v_norm_up = wv_up.norm(dim=[1, 2], keepdim=True)
w_up = wg_up * wv_up / v_norm_up
print(f"\nups[0] weight: mean={w_up.mean():.6f}, std={w_up.std():.6f}")
w_up_np = w_up.detach().numpy().astype(np.float32)
w_up_np.tofile(os.path.join(outdir, "ref_ups0_weight.bin"))
print(f"Dumped ups[0] weight: {list(w_up_np.shape)}, {w_up_np.nbytes} bytes")
