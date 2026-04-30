#!/usr/bin/env python3
"""Compare z_p layout between Go and ONNX."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto, numpy_helper

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Extract /Add_output_0 (z_p before flip) and m_p and logs_p
graph.output.append(helper.make_tensor_value_info("/Add_output_0", TensorProto.FLOAT, None))
graph.output.append(helper.make_tensor_value_info("/enc_p/Split_output_0", TensorProto.FLOAT, None))
graph.output.append(helper.make_tensor_value_info("/Transpose_3_output_0", TensorProto.FLOAT, None))
graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))

modified_path = model_path + ".zp_layout.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)
pids = np.array([[1,10,39,66,0,14,32,66,0,20,39,67,0,15,41,67,2]], dtype=np.int64)
ilen = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)

outputs = sess.run(None, {"input": pids, "input_lengths": ilen, "scales": scales})

add_out = np.array(outputs[1]).squeeze()  # /Add_output_0 = z_p before flip
m_p = np.array(outputs[2]).squeeze()      # m_p [192, 17]
transpose3 = np.array(outputs[3]).squeeze()  # Transpose_3 = m_p_expanded
path = np.array(outputs[4]).squeeze()     # path [114, 17]

print(f"/Add_output_0 (z_p before flip): shape={add_out.shape}")
print(f"  ch0[:3] = {add_out[0,:3]}")
print(f"  ch191[:3] = {add_out[191,:3]}")

print(f"\nm_p: shape={m_p.shape}")
print(f"  ch0[:3] = {m_p[0,:3]}")
print(f"  [0,0] = {m_p[0,0]}")

print(f"\nTranspose_3 (m_p_expanded): shape={transpose3.shape}")
print(f"  ch0[:3] = {transpose3[0,:3]}")
print(f"  ch191[:3] = {transpose3[191,:3]}")
print(f"  [0,0] = {transpose3[0,0]}")

print(f"\npath: shape={path.shape}")
print(f"  [0,:5] = {path[0,:5]}")
print(f"  col sums[:5] = {path.sum(axis=0)[:5]}")

# Key question: is /Add_output_0 == Transpose_3?
# (since noise_scale=0, Mul_6=0, so Add = Transpose_3 + 0 = Transpose_3)
print(f"\n/Add_output_0 == Transpose_3? {np.allclose(add_out, transpose3)}")

# Now manually compute m_p_expanded the way Go does it
# Go: ExpandByDurations(m_p, 192, 17, path, tMel)
# path is [tMel, 17], m_p is [192, 17]
# out[c, tm] = sum_tt(m_p[c, tt] * path[tm, tt])
tMel = path.shape[0]
go_expanded = np.zeros((192, tMel), dtype=np.float32)
for c in range(192):
    for tm in range(tMel):
        for tt in range(17):
            go_expanded[c, tm] += m_p[c, tt] * path[tm, tt]

print(f"\nGo-style expanded: shape={go_expanded.shape}")
print(f"  ch0[:3] = {go_expanded[0,:3]}")
print(f"  ch191[:3] = {go_expanded[191,:3]}")
print(f"  == Transpose_3? {np.allclose(go_expanded, transpose3, atol=1e-5)}")
print(f"  == /Add_output_0? {np.allclose(go_expanded, add_out, atol=1e-5)}")

# Check if the difference is just a transpose
print(f"\n  go_expanded[0,0] = {go_expanded[0,0]}")
print(f"  Transpose_3[0,0] = {transpose3[0,0]}")
print(f"  /Add_output_0[0,0] = {add_out[0,0]}")

import os; os.remove(modified_path)
