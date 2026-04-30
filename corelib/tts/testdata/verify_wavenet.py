#!/usr/bin/env python3
"""Verify WaveNet computation step by step."""
import numpy as np, torch, torch.nn.functional as F
import onnx
from onnx import numpy_helper

m = onnx.load("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")
weights = {}
for init in m.graph.initializer:
    weights[init.name] = numpy_helper.to_array(init)

# Load ONNX z_p
z_p = np.fromfile("corelib/tts/testdata/ref_piper_z_p.bin", dtype=np.float32).reshape(192, 114)
z_p_t = torch.from_numpy(z_p.copy()).unsqueeze(0)  # [1, 192, 114]

# Flip (reverse channels)
z_flipped = torch.flip(z_p_t, [1])
print(f"After flip: first 5 ch0 = {z_flipped[0, 0, :5].numpy()}")
print(f"  z_p ch191 = {z_p_t[0, 191, :5].numpy()}")  # should match flipped ch0

# Split into x0, x1
x0 = z_flipped[:, :96, :]  # [1, 96, 114]
x1 = z_flipped[:, 96:, :]  # [1, 96, 114]
print(f"x0: {x0.shape}, x1: {x1.shape}")

# Pre conv: [96, 114] → [192, 114]
# flow.flows.6 is the LAST layer in reverse (processed first)
# But in our Go code, Layers[3] = flows.6 (loaded at index 3)
# In reverse iteration: i=3 → flows.6, i=2 → flows.4, i=1 → flows.2, i=0 → flows.0
w_pre = torch.from_numpy(weights["flow.flows.6.pre.weight"].copy())
b_pre = torch.from_numpy(weights["flow.flows.6.pre.bias"].copy())
h = F.conv1d(x0, w_pre, b_pre)
print(f"After pre: {h.shape}, RMS={torch.sqrt(torch.mean(h**2)).item():.6f}")
print(f"  first 5: {h[0, 0, :5].detach().numpy()}")

# WaveNet layer 0
# in_layers.0: [384, 192, 5] dilated conv, dilation=1, padding=2
# Weight names in ONNX: onnx::Conv_8959 for flows.6.in_layers.0
# flows.6 uses Conv_8959..Conv_9052 (first 8 conv weights)
w_in0 = torch.from_numpy(weights["onnx::Conv_8959"].copy())
b_in0 = torch.from_numpy(weights["flow.flows.6.enc.in_layers.0.bias"].copy())
acts = F.conv1d(h, w_in0, b_in0, padding=2)
print(f"in_layers.0 output: {acts.shape}, RMS={torch.sqrt(torch.mean(acts**2)).item():.6f}")

# Gated activation
t_act = torch.tanh(acts[:, :192, :])
s_act = torch.sigmoid(acts[:, 192:, :])
gated = t_act * s_act
print(f"Gated: {gated.shape}, RMS={torch.sqrt(torch.mean(gated**2)).item():.6f}")

# res_skip_layers.0: [384, 192, 1]
w_rs0 = torch.from_numpy(weights["onnx::Conv_8974"].copy())
b_rs0 = torch.from_numpy(weights["flow.flows.6.enc.res_skip_layers.0.bias"].copy())
rs_out = F.conv1d(gated, w_rs0, b_rs0)
print(f"res_skip_layers.0 output: {rs_out.shape}, RMS={torch.sqrt(torch.mean(rs_out**2)).item():.6f}")

# Split: residual = rs_out[:, :192, :], skip = rs_out[:, 192:, :]
residual = rs_out[:, :192, :]
skip = rs_out[:, 192:, :]
print(f"Residual: RMS={torch.sqrt(torch.mean(residual**2)).item():.6f}")
print(f"Skip: RMS={torch.sqrt(torch.mean(skip**2)).item():.6f}")

# x = x + residual (for next layer)
h_next = h + residual
print(f"h_next (h + residual): RMS={torch.sqrt(torch.mean(h_next**2)).item():.6f}")

# Continue for all 4 layers...
output_acc = skip.clone()
h = h_next

for layer_idx in range(1, 4):
    # Map layer index to onnx::Conv weight names for flows.6
    # flows.6: Conv_8959(in0), Conv_8974(rs0), Conv_8985(in1), Conv_9000(rs1),
    #          Conv_9011(in2), Conv_9026(rs2), Conv_9037(in3), Conv_9052(rs3)
    in_conv_names = ["onnx::Conv_8959", "onnx::Conv_8985", "onnx::Conv_9011", "onnx::Conv_9037"]
    rs_conv_names = ["onnx::Conv_8974", "onnx::Conv_9000", "onnx::Conv_9026", "onnx::Conv_9052"]
    
    w_in = torch.from_numpy(weights[in_conv_names[layer_idx]].copy())
    b_in = torch.from_numpy(weights[f"flow.flows.6.enc.in_layers.{layer_idx}.bias"].copy())
    acts = F.conv1d(h, w_in, b_in, padding=2)
    t_act = torch.tanh(acts[:, :192, :])
    s_act = torch.sigmoid(acts[:, 192:, :])
    gated = t_act * s_act
    
    w_rs = torch.from_numpy(weights[rs_conv_names[layer_idx]].copy())
    b_rs = torch.from_numpy(weights[f"flow.flows.6.enc.res_skip_layers.{layer_idx}.bias"].copy())
    rs_out = F.conv1d(gated, w_rs, b_rs)
    
    if layer_idx < 3:
        residual = rs_out[:, :192, :]
        skip = rs_out[:, 192:, :]
        h = h + residual
        output_acc = output_acc + skip
    else:
        # Last layer: all goes to skip (output is [192, T])
        output_acc = output_acc + rs_out
    
    print(f"Layer {layer_idx}: h RMS={torch.sqrt(torch.mean(h**2)).item():.6f}, output_acc RMS={torch.sqrt(torch.mean(output_acc**2)).item():.6f}")

# Post conv: [192, 114] → [96, 114]
w_post = torch.from_numpy(weights["flow.flows.6.post.weight"].copy())
b_post = torch.from_numpy(weights["flow.flows.6.post.bias"].copy())
m_out = F.conv1d(output_acc, w_post, b_post)
print(f"\nPost output (m): {m_out.shape}, RMS={torch.sqrt(torch.mean(m_out**2)).item():.6f}")
print(f"  first 5: {m_out[0, 0, :5].detach().numpy()}")

# Reverse affine: x1 = x1 - m
x1_new = x1 - m_out
print(f"x1 after reverse: RMS={torch.sqrt(torch.mean(x1_new**2)).item():.6f}")

# Concat
z_after_layer6 = torch.cat([x0, x1_new], dim=1)
print(f"z after flows.6: {z_after_layer6.shape}, RMS={torch.sqrt(torch.mean(z_after_layer6**2)).item():.6f}")
print(f"  first 5: {z_after_layer6[0, 0, :5].detach().numpy()}")
