#!/usr/bin/env python3
"""Check ONNX vocoder intermediate values."""
import os, sys, numpy as np
try:
    import onnxruntime as ort
    import onnx
    from onnx import helper, numpy_helper, TensorProto
except ImportError:
    os.system(f"{sys.executable} -m pip install onnxruntime onnx -q")
    import onnxruntime as ort
    import onnx
    from onnx import helper, numpy_helper, TensorProto

model_path = os.path.join(os.path.dirname(__file__),
    "vits-piper-zh_CN-xiao_ya-medium", "zh_CN-xiao_ya-medium.onnx")

# Add intermediate outputs to capture vocoder values
model = onnx.load(model_path)
graph = model.graph

# Find the conv_post output (before tanh)
# and the ups outputs
target_outputs = []
for node in graph.node:
    # Look for the Tanh node (final activation)
    if node.op_type == "Tanh":
        # The input to Tanh is the conv_post output
        target_outputs.append(("pre_tanh", node.input[0]))
    # Look for ConvTranspose nodes (upsamples)
    if node.op_type == "ConvTranspose" and "dec" in str(node.input):
        target_outputs.append((f"convt_{node.name}", node.output[0]))

print(f"Found {len(target_outputs)} intermediate outputs to capture")

# Add them as graph outputs
for name, output_name in target_outputs:
    # Find the value info
    found = False
    for vi in graph.value_info:
        if vi.name == output_name:
            graph.output.append(vi)
            found = True
            break
    if not found:
        # Create a new output
        new_out = helper.make_tensor_value_info(output_name, TensorProto.FLOAT, None)
        graph.output.append(new_out)

# Save modified model
modified_path = model_path + ".debug.onnx"
onnx.save(model, modified_path)

# Run inference
sess = ort.InferenceSession(modified_path)
phoneme_ids = np.array([[1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2]], dtype=np.int64)
input_lengths = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)  # deterministic

outputs = sess.run(None, {
    "input": phoneme_ids,
    "input_lengths": input_lengths,
    "scales": scales,
})

print(f"\nOutputs ({len(outputs)}):")
for i, out in enumerate(outputs):
    arr = np.array(out).squeeze()
    print(f"  [{i}] shape={arr.shape}, RMS={np.sqrt(np.mean(arr**2)):.6f}, peak={np.max(np.abs(arr)):.6f}")

# Clean up
os.remove(modified_path)
