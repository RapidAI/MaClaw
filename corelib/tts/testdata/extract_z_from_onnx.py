#!/usr/bin/env python3
"""Extract z latent from ONNX model and save for Go vocoder testing."""
import os, sys, numpy as np, struct

try:
    import onnxruntime as ort
    import onnx
    from onnx import helper, TensorProto
except ImportError:
    os.system(f"{sys.executable} -m pip install onnxruntime onnx -q")
    import onnxruntime as ort
    import onnx
    from onnx import helper, TensorProto

model_path = os.path.join(os.path.dirname(__file__),
    "vits-piper-zh_CN-xiao_ya-medium", "zh_CN-xiao_ya-medium.onnx")

model = onnx.load(model_path)
graph = model.graph

# Find the Split node that produces z (input to vocoder)
# The vocoder input is after the flow decoder, which feeds into dec.conv_pre
# Look for the Mul node that applies y_mask to z
# Actually, let's find the conv_pre input
for node in graph.node:
    if node.op_type == "Conv" and any("conv_pre" in str(i) for i in node.input):
        print(f"conv_pre node: {node.name}")
        print(f"  inputs: {list(node.input)}")
        z_name = node.input[0]
        print(f"  z tensor name: {z_name}")
        break

# Add z as output
new_out = helper.make_tensor_value_info(z_name, TensorProto.FLOAT, None)
graph.output.append(new_out)

modified_path = model_path + ".z_debug.onnx"
onnx.save(model, modified_path)

# Run
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
z = outputs[1].squeeze()

print(f"z shape: {z.shape}")
print(f"z RMS: {np.sqrt(np.mean(z**2)):.6f}")
print(f"z first 5 values: {z.flatten()[:5]}")
print(f"audio shape: {audio.shape}")
print(f"audio RMS: {np.sqrt(np.mean(audio**2)):.6f}")

# Save z as binary for Go to load
z_path = os.path.join(os.path.dirname(__file__), "ref_piper_z.bin")
z.astype(np.float32).tofile(z_path)
print(f"Saved z to {z_path}: {z.shape} ({z.nbytes} bytes)")

# Also save the mel length
print(f"tMel = {z.shape[1]}")

os.remove(modified_path)
