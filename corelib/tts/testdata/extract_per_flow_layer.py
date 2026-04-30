#!/usr/bin/env python3
"""Extract z after each flow layer from ONNX runtime."""
import numpy as np
import onnxruntime as ort
import onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Add intermediate outputs after each flow layer's Concat
targets = []
for node in graph.node:
    if node.op_type == "Concat" and "/flow/flows." in node.name:
        name = node.output[0]
        targets.append((node.name, name))
        new_out = helper.make_tensor_value_info(name, TensorProto.FLOAT, None)
        graph.output.append(new_out)

# Also add the z_p (input to first flow)
for node in graph.node:
    if node.name == "/flow/flows.7/Slice":
        name = node.output[0]
        targets.insert(0, ("z_p", name))
        new_out = helper.make_tensor_value_info(name, TensorProto.FLOAT, None)
        graph.output.append(new_out)

# And the final z (vocoder input)
targets.append(("z_final", "/Mul_7_output_0"))
new_out = helper.make_tensor_value_info("/Mul_7_output_0", TensorProto.FLOAT, None)
graph.output.append(new_out)

modified_path = model_path + ".flow_layers.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
phoneme_ids = np.array([[1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2]], dtype=np.int64)
input_lengths = np.array([17], dtype=np.int64)
scales = np.array([0.667, 1.0, 0.8], dtype=np.float32)  # with noise!

outputs = sess.run(None, {
    "input": phoneme_ids,
    "input_lengths": input_lengths,
    "scales": scales,
})

print(f"Outputs: {len(outputs)}")
for i, (name, _) in enumerate(targets):
    if i + 1 < len(outputs):
        arr = outputs[i + 1].squeeze()
        print(f"  {name}: shape={arr.shape}, RMS={np.sqrt(np.mean(arr**2)):.6f}, first3={arr.flatten()[:3]}")

# Now with noise_scale=0 (deterministic)
print("\n--- Deterministic (noise_scale=0) ---")
scales_det = np.array([0.0, 1.0, 0.0], dtype=np.float32)
outputs_det = sess.run(None, {
    "input": phoneme_ids,
    "input_lengths": input_lengths,
    "scales": scales_det,
})

for i, (name, _) in enumerate(targets):
    if i + 1 < len(outputs_det):
        arr = outputs_det[i + 1].squeeze()
        print(f"  {name}: shape={arr.shape}, RMS={np.sqrt(np.mean(arr**2)):.6f}")

import os
os.remove(modified_path)
