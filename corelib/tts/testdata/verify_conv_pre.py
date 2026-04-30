#!/usr/bin/env python3
"""Verify conv_pre computation matches between ONNX and manual calculation."""
import numpy as np
import onnx
from onnx import numpy_helper

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
m = onnx.load(model_path)

# Get conv_pre weights
for init in m.graph.initializer:
    if init.name == "dec.conv_pre.weight":
        w = numpy_helper.to_array(init)  # [256, 192, 7]
    if init.name == "dec.conv_pre.bias":
        b = numpy_helper.to_array(init)  # [256]

# Load reference z
z = np.fromfile("corelib/tts/testdata/ref_piper_z.bin", dtype=np.float32).reshape(192, 114)

print(f"z: {z.shape}, w: {w.shape}, b: {b.shape}")

# Manual conv1d: padding = (7-1)/2 = 3
import torch
import torch.nn.functional as F

z_t = torch.from_numpy(z).unsqueeze(0)  # [1, 192, 114]
w_t = torch.from_numpy(w)  # [256, 192, 7]
b_t = torch.from_numpy(b)  # [256]

out = F.conv1d(z_t, w_t, b_t, padding=3)
out_np = out.detach().numpy().squeeze()  # [256, 114]

print(f"conv_pre output: {out_np.shape}")
print(f"  RMS: {np.sqrt(np.mean(out_np**2)):.6f}")
print(f"  first 5 values: {out_np.flatten()[:5]}")
print(f"  [0, :5]: {out_np[0, :5]}")

# Save for Go comparison
np.save("corelib/tts/testdata/ref_piper_conv_pre.npy", out_np)
print(f"Saved conv_pre output")
