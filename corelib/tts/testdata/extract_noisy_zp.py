#!/usr/bin/env python3
"""Extract z_p with noise from ONNX for Go testing."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph
graph.output.append(helper.make_tensor_value_info("/Add_output_0", TensorProto.FLOAT, None))
modified_path = model_path + ".noisy_zp.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
pids = np.array([[1,15,45,64,0,9,44,64,0,9,44,64,0,16,39,67,0,4,49,67,0,23,51,67,2]], dtype=np.int64)
ilen = np.array([25], dtype=np.int64)
scales = np.array([0.667, 1.0, 0.8], dtype=np.float32)  # with noise!

outputs = sess.run(None, {"input": pids, "input_lengths": ilen, "scales": scales})
audio = outputs[0].squeeze()
add_out = np.array(outputs[1]).squeeze()  # z_p before flip, with noise

print(f"ONNX audio: {len(audio)} samples, {len(audio)/22050:.2f}s")
print(f"ONNX /Add_output_0 (noisy z_p): {add_out.shape}, RMS={np.sqrt(np.mean(add_out**2)):.4f}")

add_out.astype(np.float32).tofile("corelib/tts/testdata/ref_onnx_noisy_zp_今天天气不错.bin")
print(f"Saved noisy z_p: {add_out.shape}")

import os; os.remove(modified_path)
