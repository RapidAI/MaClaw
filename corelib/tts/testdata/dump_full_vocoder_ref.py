#!/usr/bin/env python3
"""Dump full vocoder output using the same z_final, for Go comparison."""
import os, numpy as np, torch
import torch.nn.functional as F

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
ckpt = torch.load(os.path.join(model_dir, "checkpoint.pth"), map_location="cpu", weights_only=True)
sd = ckpt.get("model", ckpt)

z = np.fromfile(os.path.join(outdir, "ref_flow_04_z_final.bin"), dtype=np.float32).reshape(192, 31)
z = torch.from_numpy(z.copy()).unsqueeze(0)
g = sd["emb_g.weight"][0].unsqueeze(0).unsqueeze(-1)

# Dump after each upsample+resblock stage
with torch.no_grad():
    x = F.conv1d(z, sd["dec.conv_pre.weight"], sd["dec.conv_pre.bias"], padding=3)
    x = x + F.conv1d(g, sd["dec.cond.weight"], sd["dec.cond.bias"])

    upsample_rates = [8, 8, 2, 2, 2]
    upsample_kernel_sizes = [16, 16, 8, 2, 2]
    resblock_kernel_sizes = [3, 7, 11]
    resblock_dilation_sizes = [[1,3,5],[1,3,5],[1,3,5]]

    for i, (rate, ksize) in enumerate(zip(upsample_rates, upsample_kernel_sizes)):
        x = F.leaky_relu(x, 0.1)
        wg = sd[f"dec.ups.{i}.weight_g"]
        wv = sd[f"dec.ups.{i}.weight_v"]
        bias = sd[f"dec.ups.{i}.bias"]
        v_norm = wv.norm(dim=[1, 2], keepdim=True)
        weight = wg * wv / v_norm
        ch = weight.shape[0]
        new_ch = weight.shape[1]
        padding = (ksize - rate) // 2
        x = F.conv_transpose1d(x, weight, bias, stride=rate, padding=padding)

        xs = None
        for j, (rk, rd) in enumerate(zip(resblock_kernel_sizes, resblock_dilation_sizes)):
            idx = i * len(resblock_kernel_sizes) + j
            xr = x.clone()
            for k, dilation in enumerate(rd):
                residual = xr.clone()
                xr = F.leaky_relu(xr, 0.1)
                wg1 = sd[f"dec.resblocks.{idx}.convs1.{k}.weight_g"]
                wv1 = sd[f"dec.resblocks.{idx}.convs1.{k}.weight_v"]
                b1 = sd[f"dec.resblocks.{idx}.convs1.{k}.bias"]
                w1 = wg1 * wv1 / wv1.norm(dim=[1,2], keepdim=True)
                xr = F.conv1d(xr, w1, b1, dilation=dilation, padding=(rk-1)*dilation//2)
                xr = F.leaky_relu(xr, 0.1)
                wg2 = sd[f"dec.resblocks.{idx}.convs2.{k}.weight_g"]
                wv2 = sd[f"dec.resblocks.{idx}.convs2.{k}.weight_v"]
                b2 = sd[f"dec.resblocks.{idx}.convs2.{k}.bias"]
                w2 = wg2 * wv2 / wv2.norm(dim=[1,2], keepdim=True)
                xr = F.conv1d(xr, w2, b2, padding=(rk-1)//2)
                xr = xr + residual
            xs = xr if xs is None else xs + xr
        x = xs / len(resblock_kernel_sizes)

        data = x.squeeze(0).numpy().astype(np.float32)
        path = os.path.join(outdir, f"ref_voc_stage_{i}.bin")
        data.tofile(path)
        print(f"  stage_{i}: shape={list(data.shape)}, mean={data.mean():.4f}, std={data.std():.4f}")

    x = F.leaky_relu(x, 0.1)
    if "dec.conv_post.weight_v" in sd:
        wg = sd["dec.conv_post.weight_g"]
        wv = sd["dec.conv_post.weight_v"]
        w = wg * wv / wv.norm(dim=[1,2], keepdim=True)
        x = F.conv1d(x, w, padding=3)
    else:
        x = F.conv1d(x, sd["dec.conv_post.weight"], sd.get("dec.conv_post.bias"), padding=3)
    x = torch.tanh(x)

    audio = x.squeeze().numpy().astype(np.float32)
    audio.tofile(os.path.join(outdir, "ref_voc_audio_final.bin"))
    print(f"\n  audio: {len(audio)} samples, mean={audio.mean():.6f}, std={audio.std():.6f}, max={abs(audio).max():.6f}")
