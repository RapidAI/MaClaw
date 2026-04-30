#!/usr/bin/env python3
"""Trace the complete SDP computation from ONNX graph to understand every step."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto, numpy_helper
from collections import OrderedDict

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Add ALL dp-related intermediate outputs (float tensors only)
skip_ops = {"Shape", "Gather", "Unsqueeze", "Squeeze", "Reshape", "Cast", 
            "NonZero", "Expand", "ScatterND", "GatherND", "ConstantOfShape",
            "Equal", "Where", "GreaterOrEqual", "LessOrEqual", "And", "Not",
            "Pad", "GatherElements"}

targets = []
for node in graph.node:
    if "/dp/" in node.name and node.op_type not in skip_ops:
        for out in node.output:
            targets.append((node.name, node.op_type, out))

# Only add a subset to avoid too many outputs
key_targets = []
for name, op, out in targets:
    # Key intermediates: pre, proj, convs outputs, flow layer outputs
    if any(k in name for k in ["/dp/pre/", "/dp/proj/", "/dp/convs/convs_1x1",
            "flows.0/Exp", "flows.3/proj", "flows.5/proj", "flows.7/proj",
            "flows.7/Split", "flows.7/Softmax", "flows.7/CumSum",
            "flows.7/Softplus", "flows.7/ReduceSum",
            "flows.3/Split", "flows.3/Concat",
            "flows.5/Split", "flows.5/Concat",
            "flows.7/Concat",
            "/Exp_output", "/Ceil_output", "/ReduceSum"]):
        key_targets.append((name, op, out))
        graph.output.append(helper.make_tensor_value_info(out, TensorProto.FLOAT, None))

# Also add the final duration output
for node in graph.node:
    if node.name == "/Ceil":
        graph.output.append(helper.make_tensor_value_info(node.output[0], TensorProto.FLOAT, None))
        key_targets.append(("/Ceil", "Ceil", node.output[0]))
    if node.name == "/Exp" or node.name == "/Exp_1":
        if "/dp/" not in node.name and "/flow/" not in node.name:
            graph.output.append(helper.make_tensor_value_info(node.output[0], TensorProto.FLOAT, None))
            key_targets.append((node.name, "Exp", node.output[0]))

modified = model_path + ".sdp_trace.onnx"
onnx.save(model, modified)

try:
    sess = ort.InferenceSession(modified)
except Exception as e:
    print(f"Error loading: {e}")
    # Remove problematic outputs and retry
    import os; os.remove(modified)
    exit(1)

pids = [1,21,35,65,0,12,38,64,0,18,39,67,0,10,37,65,0,18,37,67,0,22,30,67,0,12,30,66,0,4,44,67,0,20,39,67,0,15,41,67,2]
out = sess.run(None, {"input": np.array([pids], dtype=np.int64),
                      "input_lengths": np.array([len(pids)], dtype=np.int64),
                      "scales": np.array([0.0, 1.0, 0.0], dtype=np.float32)})

print(f"Outputs: {len(out)}")
for i, (name, op, out_name) in enumerate(key_targets):
    arr = np.array(out[i+1]).squeeze()
    short = name.replace("/dp/", "dp/")
    if arr.size < 200:
        print(f"  {op:12s} {short:50s} shape={str(arr.shape):15s} vals={arr.flatten()[:5]}")
    else:
        print(f"  {op:12s} {short:50s} shape={str(arr.shape):15s} RMS={np.sqrt(np.mean(arr**2)):.4f}")

import os; os.remove(modified)
