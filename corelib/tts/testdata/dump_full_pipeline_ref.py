#!/usr/bin/env python3
"""
Full pipeline reference: encoder → duration → expand → flow → vocoder.
Uses noise_scale=0 (deterministic) for exact Go comparison.
Dumps z_final and audio for Go to compare vocoder output.
"""
import os, sys, json, math
import numpy as np
import torch
import torch.nn.functional as F

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "melotts-en")
ckpt_path = os.path.join(model_dir, "checkpoint.pth")

ckpt = torch.load(ckpt_path, map_location="cpu", weights_only=True)
sd = ckpt.get("model", ckpt)

def dump(name, tensor):
    if isinstance(tensor, torch.Tensor):
        data = tensor.detach().cpu().numpy().astype(np.float32)
    else:
        data = np.array(tensor, dtype=np.float32)
    # Remove batch dim if present
    while data.ndim > 0 and data.shape[0] == 1 and data.ndim > 1:
        data = data[0]
    path = os.path.join(outdir, f"{name}.bin")
    data.tofile(path)
    flat = data.flatten()
    print(f"  {name}: shape={list(data.shape)}, mean={flat.mean():.4f}, std={flat.std():.4f}, "
          f"min={flat.min():.4f}, max={flat.max():.4f}")

# Use the z_final from flow reference (noise_scale=0, deterministic)
z_path = os.path.join(outdir, "ref_flow_04_z_final.bin")
z = np.fromfile(z_path, dtype=np.float32).reshape(192, 31)
z = torch.from_numpy(z).unsqueeze(0)  # [1, 192, 31]
T_mel = 31

print("=== Running HiFi-GAN vocoder ===")
print(f"  z input: shape={list(z.shape)}, mean={z.mean():.4f}, std={z.std():.4f}")

# Speaker embedding
emb_g = sd["emb_g.weight"]
g = emb_g[0].unsqueeze(0).unsqueeze(-1)  # [1, 256, 1]

with torch.no_grad():
    y_mask = torch.ones(1, 1, T_mel)
    x = z * y_mask

    # conv_pre
    x = F.conv1d(x, sd["dec.conv_pre.weight"], sd["dec.conv_pre.bias"], padding=3)
    dump("ref_voc_01_conv_pre", x)

    # Speaker conditioning
    g_proj = F.conv1d(g, sd["dec.cond.weight"], sd["dec.cond.bias"])
    x = x + g_proj
    dump("ref_voc_02_after_cond", x)

    # Upsample layers
    upsample_rates = [8, 8, 2, 2, 2]
    upsample_kernel_sizes = [16, 16, 8, 2, 2]
    resblock_kernel_sizes = [3, 7, 11]
    resblock_dilation_sizes = [[1,3,5],[1,3,5],[1,3,5]]
    ch = 512

    for i, (rate, ksize) in enumerate(zip(upsample_rates, upsample_kernel_sizes)):
        x = F.leaky_relu(x, 0.1)

        # ConvTranspose1d with weight_norm
        wg = sd[f"dec.ups.{i}.weight_g"]  # [inCh, 1, 1]
        wv = sd[f"dec.ups.{i}.weight_v"]  # [inCh, outCh, kSize]
        bias = sd[f"dec.ups.{i}.bias"]

        # Reconstruct weight from weight_norm: w = g * v / ||v||
        # wg shape: [inCh, 1, 1], wv shape: [inCh, outCh, kSize]
        v_norm = wv.norm(dim=[1, 2], keepdim=True)  # [inCh, 1, 1]
        weight = wg * wv / v_norm

        new_ch = ch // 2
        padding = (ksize - rate) // 2
        x = F.conv_transpose1d(x, weight, bias, stride=rate, padding=padding)
        ch = new_ch

        dump(f"ref_voc_03_ups_{i}", x)

        # ResBlocks
        xs = None
        for j, (rk, rd) in enumerate(zip(resblock_kernel_sizes, resblock_dilation_sizes)):
            idx = i * len(resblock_kernel_sizes) + j
            xr = x.clone()

            for k, dilation in enumerate(rd):
                xr = F.leaky_relu(xr, 0.1)
                # convs1[k] with weight_norm
                wg1 = sd[f"dec.resblocks.{idx}.convs1.{k}.weight_g"]
                wv1 = sd[f"dec.resblocks.{idx}.convs1.{k}.weight_v"]
                b1 = sd[f"dec.resblocks.{idx}.convs1.{k}.bias"]
                v_norm1 = wv1.norm(dim=[1, 2], keepdim=True)
                w1 = wg1 * wv1 / v_norm1
                pad1 = (rk - 1) * dilation // 2
                xr = F.conv1d(xr, w1, b1, dilation=dilation, padding=pad1)

                xr = F.leaky_relu(xr, 0.1)
                # convs2[k] with weight_norm
                wg2 = sd[f"dec.resblocks.{idx}.convs2.{k}.weight_g"]
                wv2 = sd[f"dec.resblocks.{idx}.convs2.{k}.weight_v"]
                b2 = sd[f"dec.resblocks.{idx}.convs2.{k}.bias"]
                v_norm2 = wv2.norm(dim=[1, 2], keepdim=True)
                w2 = wg2 * wv2 / v_norm2
                pad2 = (rk - 1) // 2
                xr = F.conv1d(xr, w2, b2, padding=pad2)

            if xs is None:
                xs = xr
            else:
                xs = xs + xr

        x = xs / len(resblock_kernel_sizes)

    x = F.leaky_relu(x, 0.1)

    # conv_post (also has weight_norm in some models, check)
    if "dec.conv_post.weight_v" in sd:
        wg = sd["dec.conv_post.weight_g"]
        wv = sd["dec.conv_post.weight_v"]
        v_norm = wv.norm(dim=[1, 2], keepdim=True)
        w = wg * wv / v_norm
        x = F.conv1d(x, w, padding=3)
    elif "dec.conv_post.weight" in sd:
        x = F.conv1d(x, sd["dec.conv_post.weight"],
                     sd.get("dec.conv_post.bias"), padding=3)

    x = torch.tanh(x)
    dump("ref_voc_final_audio", x)

    # Save as WAV
    audio = x.squeeze().numpy()
    print(f"\n  Final audio: {len(audio)} samples, {len(audio)/22050:.2f} sec")
    audio_int16 = np.clip(audio * 32767, -32768, 32767).astype(np.int16)
    import scipy.io.wavfile as wavfile
    wav_path = os.path.join(outdir, "ref_hello_en_python.wav")
    wavfile.write(wav_path, 22050, audio_int16)
    print(f"  Saved: {wav_path}")
