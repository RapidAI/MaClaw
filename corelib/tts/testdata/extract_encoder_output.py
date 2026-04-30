#!/usr/bin/env python3
"""Extract text encoder output from ONNX for comparison."""
import numpy as np
import onnxruntime as ort
import onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Find the encoder proj output (m_p, logs_p)
# It's the Split that produces m_p and logs_p
for node in graph.node:
    if node.name == "/enc_p/Split":
        print(f"enc_p Split: inputs={list(node.input)}, outputs={list(node.output)}")
        # Add both outputs
        for out_name in node.output:
            new_out = helper.make_tensor_value_info(out_name, TensorProto.FLOAT, None)
            graph.output.append(new_out)
        # Also add the input (before split)
        proj_out = node.input[0]
        new_out = helper.make_tensor_value_info(proj_out, TensorProto.FLOAT, None)
        graph.output.append(new_out)
        break

modified_path = model_path + ".enc_debug.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
phoneme_ids = np.array([[1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2]], dtype=np.int64)
input_lengths = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)

outputs = sess.run(None, {
    "input": phoneme_ids,
    "input_lengths": input_lengths,
    "scales": scales,
})

audio = outputs[0].squeeze()
m_p = outputs[1].squeeze()
logs_p = outputs[2].squeeze()
proj_out = outputs[3].squeeze()

print(f"m_p: {m_p.shape}, RMS={np.sqrt(np.mean(m_p**2)):.6f}")
print(f"logs_p: {logs_p.shape}, RMS={np.sqrt(np.mean(logs_p**2)):.6f}")
print(f"m_p first 5: {m_p.flatten()[:5]}")
print(f"logs_p first 5: {logs_p.flatten()[:5]}")

# Save for Go comparison
m_p.astype(np.float32).tofile("corelib/tts/testdata/ref_piper_m_p.bin")
logs_p.astype(np.float32).tofile("corelib/tts/testdata/ref_piper_logs_p.bin")
print(f"\nSaved m_p {m_p.shape} and logs_p {logs_p.shape}")

import os
os.remove(modified_path)
