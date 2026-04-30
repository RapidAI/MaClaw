#!/usr/bin/env python3
"""Verify full flow decoder layer by layer."""
import numpy as np, torch, torch.nn.functional as F
import onnx
from onnx import numpy_helper

m = onnx.load("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")
weights = {}
for init in m.graph.initializer:
    weights[init.name] = numpy_helper.to_array(init)

z_p = np.fromfile("corelib/tts/testdata/ref_piper_z_p.bin", dtype=np.float32).reshape(192, 114)
z = torch.from_numpy(z_p.copy()).unsqueeze(0)

# Flow layer indices and their onnx::Conv weight mapping
# ONNX processes: flows.6 → flows.4 → flows.2 → flows.0
# Conv weights per flow layer (8 each: 4 in_layers + 4 res_skip_layers)
flow_conv_map = {
    6: [8959, 8974, 8985, 9000, 9011, 9026, 9037, 9052],
    4: [9055, 9070, 9081, 9096, 9107, 9122, 9133, 9148],
    2: [9151, 9166, 9177, 9192, 9203, 9218, 9229, 9244],
    0: [9247, 9262, 9273, 9288, 9299, 9314, 9325, 9340],
}

for flow_idx in [6, 4, 2, 0]:
    # Flip
    z = torch.flip(z, [1])
    
    # Split
    x0 = z[:, :96, :]
    x1 = z[:, 96:, :]
    
    # Pre
    w_pre = torch.from_numpy(weights[f"flow.flows.{flow_idx}.pre.weight"].copy())
    b_pre = torch.from_numpy(weights[f"flow.flows.{flow_idx}.pre.bias"].copy())
    h = F.conv1d(x0, w_pre, b_pre)
    
    # WaveNet
    convs = flow_conv_map[flow_idx]
    output_acc = torch.zeros_like(h)
    
    for wn_layer in range(4):
        in_conv_id = convs[wn_layer * 2]
        rs_conv_id = convs[wn_layer * 2 + 1]
        
        w_in = torch.from_numpy(weights[f"onnx::Conv_{in_conv_id}"].copy())
        b_in = torch.from_numpy(weights[f"flow.flows.{flow_idx}.enc.in_layers.{wn_layer}.bias"].copy())
        acts = F.conv1d(h, w_in, b_in, padding=2)
        
        t_act = torch.tanh(acts[:, :192, :])
        s_act = torch.sigmoid(acts[:, 192:, :])
        gated = t_act * s_act
        
        w_rs = torch.from_numpy(weights[f"onnx::Conv_{rs_conv_id}"].copy())
        b_rs = torch.from_numpy(weights[f"flow.flows.{flow_idx}.enc.res_skip_layers.{wn_layer}.bias"].copy())
        rs_out = F.conv1d(gated, w_rs, b_rs)
        
        if wn_layer < 3:
            h = h + rs_out[:, :192, :]
            output_acc = output_acc + rs_out[:, 192:, :]
        else:
            output_acc = output_acc + rs_out
    
    # Post
    w_post = torch.from_numpy(weights[f"flow.flows.{flow_idx}.post.weight"].copy())
    b_post = torch.from_numpy(weights[f"flow.flows.{flow_idx}.post.bias"].copy())
    m_out = F.conv1d(output_acc, w_post, b_post)
    
    # Reverse: x1 = x1 - m
    x1 = x1 - m_out
    
    # Concat
    z = torch.cat([x0, x1], dim=1)
    
    rms = torch.sqrt(torch.mean(z**2)).item()
    print(f"After flows.{flow_idx}: z RMS={rms:.6f}")

print(f"\nFinal z first 5: {z[0, 0, :5].detach().numpy()}")
