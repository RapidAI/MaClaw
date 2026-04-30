#!/usr/bin/env python3
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

targets = [
    "/flow/flows.7/Slice_output_0",  # after flip
    "/flow/flows.6/Split_output_0",  # x0
    "/flow/flows.6/Split_output_1",  # x1
    "/flow/flows.6/Concat_output_0", # after coupling
]
for t in targets:
    graph.output.append(helper.make_tensor_value_info(t, TensorProto.FLOAT, None))

modified_path = model_path + ".split_debug.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
pids = np.array([[1,10,39,66,0,14,32,66,0,20,39,67,0,15,41,67,2]], dtype=np.int64)
ilen = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)

outputs = sess.run(None, {"input": pids, "input_lengths": ilen, "scales": scales})

labels = ["after_flip", "x0", "x1", "after_concat"]
for i, label in enumerate(labels):
    arr = np.array(outputs[i+1]).squeeze()
    print(f"{label}: shape={arr.shape}, ch0[:3]={arr[0,:3]}, ch_last[:3]={arr[-1,:3]}, RMS={np.sqrt(np.mean(arr**2)):.4f}")

import os; os.remove(modified_path)
