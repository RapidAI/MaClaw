#!/usr/bin/env python3
"""Verify ups[0] ConvTranspose1d output."""
import numpy as np
import torch
import torch.nn.functional as F
import onnx
from onnx import numpy_helper

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
m = onnx.load(model_path)

# Get weights
weights = {}
for init in m.graph.initializer:
    if init.name.startswith("dec."):
        weights[init.name] = numpy_helper.to_array(init)

# Load conv_pre output
conv_pre_out = np.load("corelib/tts/testdata/ref_piper_conv_pre.npy")  # [256, 114]
print(f"conv_pre_out: {conv_pre_out.shape}, RMS={np.sqrt(np.mean(conv_pre_out**2)):.6f}")

# Apply LeakyReLU
x = torch.from_numpy(conv_pre_out).unsqueeze(0).clone()  # [1, 256, 114]
x = F.leaky_relu(x, 0.1)
print(f"After LeakyReLU: RMS={torch.sqrt(torch.mean(x**2)).item():.6f}")

# ConvTranspose1d: ups[0]
w_ups0 = torch.from_numpy(weights["dec.ups.0.weight"].copy())  # [256, 128, 16]
b_ups0 = torch.from_numpy(weights["dec.ups.0.bias"].copy())    # [128]
stride = 8
padding = (16 - 8) // 2  # = 4

x_up = F.conv_transpose1d(x, w_ups0, b_ups0, stride=stride, padding=padding)
print(f"After ups[0]: {x_up.shape}, RMS={torch.sqrt(torch.mean(x_up**2)).item():.6f}")

# ResBlocks
# resblocks 0, 1, 2 (kernel sizes 3, 5, 7)
rb_sum = None
for j in range(3):
    idx = j
    xc = x_up.clone()
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

rb_avg = rb_sum / 3.0
print(f"After resblocks avg: {rb_avg.shape}, RMS={torch.sqrt(torch.mean(rb_avg**2)).item():.6f}")

# Save for comparison
np.save("corelib/tts/testdata/ref_piper_after_ups0.npy", rb_avg.detach().numpy().squeeze())
