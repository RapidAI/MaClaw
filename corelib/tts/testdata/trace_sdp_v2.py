#!/usr/bin/env python3
"""Trace SDP by running Python implementation matching ONNX graph exactly."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto, numpy_helper

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)

# Load all weights
weights = {}
for init in model.graph.initializer:
    weights[init.name] = numpy_helper.to_array(init)

# Add encoder output as graph output
model.graph.output.append(helper.make_tensor_value_info("/enc_p/Split_output_0", TensorProto.FLOAT, None))
model.graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))
modified = model_path + ".sdp2.onnx"
onnx.save(model, modified)
sess = ort.InferenceSession(modified)

pids = [1,21,35,65,0,12,38,64,0,18,39,67,0,10,37,65,0,18,37,67,0,22,30,67,0,12,30,66,0,4,44,67,0,20,39,67,0,15,41,67,2]
out = sess.run(None, {"input": np.array([pids], dtype=np.int64),
                      "input_lengths": np.array([len(pids)], dtype=np.int64),
                      "scales": np.array([0.0, 1.0, 0.0], dtype=np.float32)})

m_p = np.array(out[1]).squeeze()  # [192, 41]
path = np.array(out[2]).squeeze()  # [tMel, 41]
durations = path.sum(axis=0).astype(int)
print(f"ONNX durations: {list(durations)}")
print(f"tMel: {path.shape[0]}")

# Now implement SDP in Python step by step
import torch
import torch.nn.functional as F

T = len(pids)
hidden = 192

# Step 1: dp.pre — condition on encoder output x (which equals m_p before split... 
# actually SDP input is the encoder hidden state BEFORE proj, not m_p)
# Let me check what feeds into dp.pre
print("\n=== Tracing SDP input ===")
for node in model.graph.node:
    if node.name == "/dp/pre/Conv":
        print(f"dp.pre input: {node.input[0]}")
        # This tells us what tensor feeds into the SDP

import os; os.remove(modified)
