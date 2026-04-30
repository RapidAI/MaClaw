#!/usr/bin/env python3
"""Save ONNX path matrix and /Add_output_0 for Go testing."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

graph.output.append(helper.make_tensor_value_info("/Add_output_0", TensorProto.FLOAT, None))
graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))

modified_path = model_path + ".save.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
pids = np.array([[1,10,39,66,0,14,32,66,0,20,39,67,0,15,41,67,2]], dtype=np.int64)
ilen = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)

outputs = sess.run(None, {"input": pids, "input_lengths": ilen, "scales": scales})

add_out = np.array(outputs[1]).squeeze()  # [192, 114] = z_p before flip
path = np.array(outputs[2]).squeeze()     # [114, 17]

print(f"/Add_output_0: {add_out.shape}, RMS={np.sqrt(np.mean(add_out**2)):.6f}")
print(f"path: {path.shape}")
print(f"path col sums (durations): {path.sum(axis=0).astype(int)}")

add_out.astype(np.float32).tofile("corelib/tts/testdata/ref_onnx_add_output.bin")
path.astype(np.float32).tofile("corelib/tts/testdata/ref_onnx_path.bin")
print("Saved /Add_output_0 and path")

import os; os.remove(modified_path)
