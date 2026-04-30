#!/usr/bin/env python3
"""Step-by-step vocoder verification: compute each stage in Python and save for Go comparison."""
import numpy as np
import torch
import torch.nn.functional as F
import onnx
from onnx import numpy_helper

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
m = onnx.load(model_path)
weights = {}
for init in m.graph.initializer:
    if init.name.startswith("dec."):
        weights[init.name] = numpy_helper.to_array(init)

# Load reference z
z = np.fromfile("corelib/tts/testdata/ref_piper_z.bin", dtype=np.float32).reshape(192, 114)
x = torch.from_numpy(z.copy()).unsqueeze(0)  # [1, 192, 114]

# conv_pre
w_cp = torch.from_numpy(weights["dec.conv_pre.weight"].copy())
b_cp = torch.from_numpy(weights["dec.conv_pre.bias"].copy())
x = F.conv1d(x, w_cp, b_cp, padding=3)
print(f"conv_pre: {x.shape}, RMS={torch.sqrt(torch.mean(x**2)).item():.6f}")

ups_rates = [8, 8, 4]
ups_ksizes = [16, 16, 8]
rb_kernels = [3, 5, 7]
ch = 256

for i, (rate, ksize) in enumerate(zip(ups_rates, ups_ksizes)):
    # LeakyReLU
    x = F.leaky_relu(x, 0.1)
    
    # ConvTranspose1d
    newCh = ch // 2
    w_up = torch.from_numpy(weights[f"dec.ups.{i}.weight"].copy())
    b_up = torch.from_numpy(weights[f"dec.ups.{i}.bias"].copy())
    pad = (ksize - rate) // 2
    x = F.conv_transpose1d(x, w_up, b_up, stride=rate, padding=pad)
    ch = newCh
    print(f"ups[{i}] ConvT: {x.shape}, RMS={torch.sqrt(torch.mean(x**2)).item():.6f}")
    
    # ResBlocks
    rb_sum = None
    for j in range(3):
        idx = i * 3 + j
        xc = x.clone()
        for k in range(2):
            residual = xc.clone()
            xc = F.leaky_relu(xc, 0.1)
            w_rb = torch.from_numpy(weights[f"dec.resblocks.{idx}.convs.{k}.weight"].copy())
            b_rb = torch.from_numpy(weights[f"dec.resblocks.{idx}.convs.{k}.bias"].copy())
            pad = (w_rb.shape[2] - 1) // 2
            xc = F.conv1d(xc, w_rb, b_rb, padding=pad)
            xc = xc + residual
        if rb_sum is None:
            rb_sum = xc
        else:
            rb_sum = rb_sum + xc
    x = rb_sum / 3.0
    print(f"ups[{i}] + RB: {x.shape}, RMS={torch.sqrt(torch.mean(x**2)).item():.6f}")

# Final
x = F.leaky_relu(x, 0.1)
print(f"final LeakyReLU: RMS={torch.sqrt(torch.mean(x**2)).item():.6f}, neg%={100*(x<0).float().mean().item():.1f}%")

w_post = torch.from_numpy(weights["dec.conv_post.weight"].copy())
audio = F.conv1d(x, w_post, padding=3)
print(f"conv_post (before tanh): {audio.shape}, RMS={torch.sqrt(torch.mean(audio**2)).item():.6f}, peak={audio.abs().max().item():.6f}")

audio = torch.tanh(audio)
print(f"final audio: RMS={torch.sqrt(torch.mean(audio**2)).item():.6f}, peak={audio.abs().max().item():.6f}")

# Save stage outputs as binary for Go comparison
for name, arr in [
    ("ref_piper_voc_after_ups0_rb", None),  # placeholder
]:
    pass

# Key finding: what's the neg% at each stage?
print("\n=== Negative value distribution ===")
# Redo with tracking
x = torch.from_numpy(z.copy()).unsqueeze(0)
x = F.conv1d(x, torch.from_numpy(weights["dec.conv_pre.weight"].copy()),
             torch.from_numpy(weights["dec.conv_pre.bias"].copy()), padding=3)
ch = 256
for i, (rate, ksize) in enumerate(zip(ups_rates, ups_ksizes)):
    x = F.leaky_relu(x, 0.1)
    newCh = ch // 2
    x = F.conv_transpose1d(x, torch.from_numpy(weights[f"dec.ups.{i}.weight"].copy()),
                           torch.from_numpy(weights[f"dec.ups.{i}.bias"].copy()),
                           stride=rate, padding=(ksize-rate)//2)
    ch = newCh
    neg_pct = 100 * (x < 0).float().mean().item()
    print(f"  After ConvT[{i}]: neg%={neg_pct:.1f}%")
    
    rb_sum = None
    for j in range(3):
        idx = i * 3 + j
        xc = x.clone()
        for k in range(2):
            residual = xc.clone()
            xc = F.leaky_relu(xc, 0.1)
            w_rb = torch.from_numpy(weights[f"dec.resblocks.{idx}.convs.{k}.weight"].copy())
            b_rb = torch.from_numpy(weights[f"dec.resblocks.{idx}.convs.{k}.bias"].copy())
            xc = F.conv1d(xc, w_rb, b_rb, padding=(w_rb.shape[2]-1)//2)
            xc = xc + residual
        if rb_sum is None:
            rb_sum = xc
        else:
            rb_sum = rb_sum + xc
    x = rb_sum / 3.0
    neg_pct = 100 * (x < 0).float().mean().item()
    print(f"  After RB[{i}]: neg%={neg_pct:.1f}%")

neg_pct = 100 * (x < 0).float().mean().item()
x_lrelu = F.leaky_relu(x, 0.1)
print(f"  Before final LReLU: neg%={neg_pct:.1f}%")
print(f"  After final LReLU: RMS={torch.sqrt(torch.mean(x_lrelu**2)).item():.6f}")
