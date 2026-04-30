#!/usr/bin/env python3
"""Trace flows.6 key intermediates from ONNX."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Only add Conv/Mul/Add/Split/Concat/Sub outputs (guaranteed float)
skip_ops = {"Shape", "ConstantOfShape", "Gather", "Unsqueeze", "Squeeze", "Reshape", "Cast", "NonZero", "Expand", "ScatterND", "GatherElements", "Pad"}
targets = []
for node in graph.node:
    if "/flow/flows.6/" in node.name or node.name == "/flow/flows.7/Slice":
        if node.op_type in skip_ops:
            continue
        for out in node.output:
            targets.append((node.name, node.op_type, out))
            graph.output.append(helper.make_tensor_value_info(out, TensorProto.FLOAT, None))

modified_path = model_path + ".trace6v2.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
pids = np.array([[1,10,39,66,0,14,32,66,0,20,39,67,0,15,41,67,2]], dtype=np.int64)
ilen = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)

outputs = sess.run(None, {"input": pids, "input_lengths": ilen, "scales": scales})

for i, (name, op, out_name) in enumerate(targets):
    arr = np.array(outputs[i+1]).squeeze()
    short = name.replace("/flow/flows.6/", "").replace("/flow/flows.7/", "f7/")
    print(f"  {op:8s} {short:40s} shape={str(arr.shape):15s} RMS={np.sqrt(np.mean(arr**2)):.6f}  first3={arr.flatten()[:3]}")

import os; os.remove(modified_path)
