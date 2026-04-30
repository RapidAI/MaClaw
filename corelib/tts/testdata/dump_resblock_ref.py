#!/usr/bin/env python3
"""Dump resblock 0 intermediate outputs for debugging."""
import os, numpy as np, torch
import torch.nn.functional as F

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
ckpt = torch.load(os.path.join(model_dir, "checkpoint.pth"), map_location="cpu", weights_only=True)
sd = ckpt.get("model", ckpt)

def dump(name, tensor):
    data = tensor.detach().cpu().numpy().astype(np.float32)
    while data.ndim > 0 and data.shape[0] == 1 and data.ndim > 1:
        data = data[0]
    path = os.path.join(outdir, f"{name}.bin")
    data.tofile(path)
    flat = data.flatten()
    print(f"  {name}: shape={list(data.shape)}, mean={flat.mean():.4f}, std={flat.std():.4f}")

# Load ups_0 output as input to resblocks
ups0 = np.fromfile(os.path.join(outdir, "ref_voc_03_ups_0.bin"), dtype=np.float32)
x = torch.from_numpy(ups0.copy()).unsqueeze(0).reshape(1, 256, 248)
ch = 256
T = 248

print(f"Input to resblock 0: shape={list(x.shape)}, mean={x.mean():.4f}, std={x.std():.4f}")

# ResBlock 0 (kernel_size=3, dilations=[1,3,5])
dilations = [1, 3, 5]
idx = 0  # resblock index

with torch.no_grad():
    for k, dilation in enumerate(dilations):
        residual = x.clone()

        # LeakyReLU
        x = F.leaky_relu(x, 0.1)

        # convs1[k] with weight_norm
        wg1 = sd[f"dec.resblocks.{idx}.convs1.{k}.weight_g"]
        wv1 = sd[f"dec.resblocks.{idx}.convs1.{k}.weight_v"]
        b1 = sd[f"dec.resblocks.{idx}.convs1.{k}.bias"]
        v_norm1 = wv1.norm(dim=[1, 2], keepdim=True)
        w1 = wg1 * wv1 / v_norm1
        pad1 = (3 - 1) * dilation // 2  # kernel_size=3
        y = F.conv1d(x, w1, b1, dilation=dilation, padding=pad1)
        dump(f"ref_rb0_convs1_{k}", y)

        # LeakyReLU
        y = F.leaky_relu(y, 0.1)

        # convs2[k] with weight_norm
        wg2 = sd[f"dec.resblocks.{idx}.convs2.{k}.weight_g"]
        wv2 = sd[f"dec.resblocks.{idx}.convs2.{k}.weight_v"]
        b2 = sd[f"dec.resblocks.{idx}.convs2.{k}.bias"]
        v_norm2 = wv2.norm(dim=[1, 2], keepdim=True)
        w2 = wg2 * wv2 / v_norm2
        pad2 = (3 - 1) // 2  # kernel_size=3, dilation=1
        z = F.conv1d(y, w2, b2, padding=pad2)
        dump(f"ref_rb0_convs2_{k}", z)

        # Residual
        x = residual + z
        dump(f"ref_rb0_after_pair_{k}", x)

    dump("ref_rb0_output", x)
