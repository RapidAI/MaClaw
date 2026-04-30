#!/usr/bin/env python3
"""Compare Go and ONNX flow layer 0 (flows.6) output precisely."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto

# Get ONNX flows.6 output
model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Add flows.6 Concat output
graph.output.append(helper.make_tensor_value_info("/flow/flows.6/Concat_output_0", TensorProto.FLOAT, None))
# Add z_p (before any flow)
graph.output.append(helper.make_tensor_value_info("/flow/flows.7/Slice_output_0", TensorProto.FLOAT, None))

modified_path = model_path + ".cmp.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
pids = np.array([[1,10,39,66,0,14,32,66,0,20,39,67,0,15,41,67,2]], dtype=np.int64)
ilen = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)

outputs = sess.run(None, {"input": pids, "input_lengths": ilen, "scales": scales})
onnx_after6 = np.array(outputs[1]).squeeze()  # [192, 114]
onnx_zp = np.array(outputs[2]).squeeze()  # [192, 114]

print(f"ONNX z_p: {onnx_zp.shape}, RMS={np.sqrt(np.mean(onnx_zp**2)):.6f}")
print(f"ONNX after flows.6: {onnx_after6.shape}, RMS={np.sqrt(np.mean(onnx_after6**2)):.6f}")

# Now compute flows.6 manually in Python using the ONNX z_p
import torch, torch.nn.functional as F
from onnx import numpy_helper

weights = {}
for init in model.graph.initializer:
    weights[init.name] = numpy_helper.to_array(init)

z = torch.from_numpy(onnx_zp.copy()).unsqueeze(0)  # [1, 192, 114]

# Flip (this is flows.7/Slice)
z_flipped = torch.flip(z, [1])

# Split
x0 = z_flipped[:, :96, :]
x1 = z_flipped[:, 96:, :]

# Pre
w_pre = torch.from_numpy(weights["flow.flows.6.pre.weight"].copy())
b_pre = torch.from_numpy(weights["flow.flows.6.pre.bias"].copy())
h = F.conv1d(x0, w_pre, b_pre)

# WaveNet (4 layers)
conv_ids = [8959, 8974, 8985, 9000, 9011, 9026, 9037, 9052]
output_acc = torch.zeros_like(h)
for wn_layer in range(4):
    w_in = torch.from_numpy(weights[f"onnx::Conv_{conv_ids[wn_layer*2]}"].copy())
    b_in = torch.from_numpy(weights[f"flow.flows.6.enc.in_layers.{wn_layer}.bias"].copy())
    acts = F.conv1d(h, w_in, b_in, padding=2)
    t_act = torch.tanh(acts[:, :192, :])
    s_act = torch.sigmoid(acts[:, 192:, :])
    gated = t_act * s_act
    
    w_rs = torch.from_numpy(weights[f"onnx::Conv_{conv_ids[wn_layer*2+1]}"].copy())
    b_rs = torch.from_numpy(weights[f"flow.flows.6.enc.res_skip_layers.{wn_layer}.bias"].copy())
    rs_out = F.conv1d(gated, w_rs, b_rs)
    
    if wn_layer < 3:
        h = h + rs_out[:, :192, :]
        output_acc = output_acc + rs_out[:, 192:, :]
    else:
        output_acc = output_acc + rs_out

# Post
w_post = torch.from_numpy(weights["flow.flows.6.post.weight"].copy())
b_post = torch.from_numpy(weights["flow.flows.6.post.bias"].copy())
m_out = F.conv1d(output_acc, w_post, b_post)

# Reverse: x1 = x1 - m
x1_new = x1 - m_out
py_after6 = torch.cat([x0, x1_new], dim=1).squeeze().detach().numpy()

print(f"\nPython after flows.6: RMS={np.sqrt(np.mean(py_after6**2)):.6f}")
print(f"ONNX after flows.6:   RMS={np.sqrt(np.mean(onnx_after6**2)):.6f}")

# Compare
diff = np.abs(py_after6 - onnx_after6)
print(f"\nPython vs ONNX: maxDiff={diff.max():.8f}, meanDiff={diff.mean():.8f}")
print(f"Correlation: {np.corrcoef(py_after6.flatten(), onnx_after6.flatten())[0,1]:.8f}")

# Check specific channels
for ch in [0, 50, 95, 96, 150, 191]:
    py_rms = np.sqrt(np.mean(py_after6[ch]**2))
    onnx_rms = np.sqrt(np.mean(onnx_after6[ch]**2))
    print(f"  ch{ch}: py_rms={py_rms:.6f}, onnx_rms={onnx_rms:.6f}, ratio={onnx_rms/py_rms:.3f}")

import os; os.remove(modified_path)
