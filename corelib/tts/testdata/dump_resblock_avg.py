#!/usr/bin/env python3
"""Dump the average of 3 resblocks (after ups_0) for comparison."""
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

ups0 = np.fromfile(os.path.join(outdir, "ref_voc_03_ups_0.bin"), dtype=np.float32)
x = torch.from_numpy(ups0.copy()).unsqueeze(0).reshape(1, 256, 248)

resblock_kernel_sizes = [3, 7, 11]
resblock_dilation_sizes = [[1,3,5],[1,3,5],[1,3,5]]

with torch.no_grad():
    xs = None
    for j, (rk, rd) in enumerate(zip(resblock_kernel_sizes, resblock_dilation_sizes)):
        xr = x.clone()
        for k, dilation in enumerate(rd):
            residual = xr.clone()
            xr = F.leaky_relu(xr, 0.1)
            wg1 = sd[f"dec.resblocks.{j}.convs1.{k}.weight_g"]
            wv1 = sd[f"dec.resblocks.{j}.convs1.{k}.weight_v"]
            b1 = sd[f"dec.resblocks.{j}.convs1.{k}.bias"]
            v_norm1 = wv1.norm(dim=[1, 2], keepdim=True)
            w1 = wg1 * wv1 / v_norm1
            pad1 = (rk - 1) * dilation // 2
            xr = F.conv1d(xr, w1, b1, dilation=dilation, padding=pad1)
            xr = F.leaky_relu(xr, 0.1)
            wg2 = sd[f"dec.resblocks.{j}.convs2.{k}.weight_g"]
            wv2 = sd[f"dec.resblocks.{j}.convs2.{k}.weight_v"]
            b2 = sd[f"dec.resblocks.{j}.convs2.{k}.bias"]
            v_norm2 = wv2.norm(dim=[1, 2], keepdim=True)
            w2 = wg2 * wv2 / v_norm2
            pad2 = (rk - 1) // 2
            xr = F.conv1d(xr, w2, b2, padding=pad2)
            xr = xr + residual
        dump(f"ref_rb_individual_{j}", xr)
        if xs is None:
            xs = xr
        else:
            xs = xs + xr
    x_avg = xs / len(resblock_kernel_sizes)
    dump("ref_rb_avg_after_ups0", x_avg)
