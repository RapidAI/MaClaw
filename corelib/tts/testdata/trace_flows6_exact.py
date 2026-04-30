#!/usr/bin/env python3
"""Trace flows.6 computation exactly from ONNX graph, extracting every intermediate."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Collect ALL flows.6 intermediate outputs
targets = []
for node in graph.node:
    if "/flow/flows.6/" in node.name or "/flow/flows.7/" in node.name:
        for out in node.output:
            targets.append(out)
            graph.output.append(helper.make_tensor_value_info(out, TensorProto.FLOAT, None))

modified_path = model_path + ".trace6.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
pids = np.array([[1,10,39,66,0,14,32,66,0,20,39,67,0,15,41,67,2]], dtype=np.int64)
ilen = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)

outputs = sess.run(None, {"input": pids, "input_lengths": ilen, "scales": scales})

# Print key intermediates
print("=== flows.7 (Flip) ===")
for i, name in enumerate(targets):
    arr = np.array(outputs[i+1]).squeeze()
    if "flows.7" in name:
        print(f"  {name}: shape={arr.shape}, RMS={np.sqrt(np.mean(arr**2)):.6f}, [:3]={arr.flatten()[:3]}")

print("\n=== flows.6 key intermediates ===")
for i, name in enumerate(targets):
    arr = np.array(outputs[i+1]).squeeze()
    if "flows.6" not in name:
        continue
    short = name.split("/flow/flows.6/")[-1] if "/flow/flows.6/" in name else name
    if any(k in short for k in ["Split", "pre/Conv", "Mul_output", "in_layers.0", 
            "res_skip_layers.0/Conv", "Slice_output", "Slice_1_output",
            "Add_output", "Add_1_output", "Add_6", "Mul_3", "post/Conv",
            "Concat", "Sub", "Neg", "Exp"]):
        if arr.size < 100000:
            print(f"  {short}: shape={arr.shape}, RMS={np.sqrt(np.mean(arr**2)):.6f}, [:3]={arr.flatten()[:3]}")
        else:
            print(f"  {short}: shape={arr.shape}, RMS={np.sqrt(np.mean(arr**2)):.6f}")

import os; os.remove(modified_path)
