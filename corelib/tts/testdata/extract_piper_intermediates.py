#!/usr/bin/env python3
"""Extract intermediate tensors from Piper ONNX model for debugging Go implementation."""
import os
import sys
import numpy as np

try:
    import onnxruntime as ort
    import onnx
    from onnx import numpy_helper
except ImportError:
    os.system(f"{sys.executable} -m pip install onnxruntime onnx -q")
    import onnxruntime as ort
    import onnx

model_dir = os.path.join(os.path.dirname(__file__), "vits-piper-zh_CN-xiao_ya-medium")
model_path = os.path.join(model_dir, "zh_CN-xiao_ya-medium.onnx")

# We need to add intermediate outputs to the ONNX model to extract them.
# Key intermediate nodes to capture:
# 1. After embedding (enc_p.emb output)
# 2. After encoder (before proj)
# 3. After proj (m_p, logs_p)
# 4. Duration predictor output (logw / durations)
# 5. After flow decoder (z)
# 6. Vocoder input

# For now, let's just run the full model and check the output shape
# to understand the expected mel length for "你好世界"

# Use onnxruntime with all outputs
model = onnx.load(model_path)
graph = model.graph

# Find key intermediate node outputs by searching for specific patterns
print("Looking for key intermediate outputs...")

# Find the Split node that splits m_p and logs_p
for node in graph.node:
    if node.op_type == "Split" and any("enc_p" in str(i) for i in node.input):
        print(f"  Split (m_p/logs_p): {node.name}")
        print(f"    inputs: {list(node.input)}")
        print(f"    outputs: {list(node.output)}")

# Find the ReduceSum that computes total duration
for node in graph.node:
    if node.op_type == "ReduceSum":
        print(f"  ReduceSum: {node.name}")
        print(f"    inputs: {list(node.input)}")
        print(f"    outputs: {list(node.output)}")

# Let's try a simpler approach: run with deterministic settings and
# extract the embedding table values to verify our Go loading is correct
print("\n=== Verifying embedding table ===")
onnx_model = onnx.load(model_path)
for init in onnx_model.graph.initializer:
    if init.name == "sid":
        arr = numpy_helper.to_array(init)
        print(f"sid shape: {arr.shape}")
        # Print first few values for phoneme ID 1 (^) and 10 (n)
        print(f"  sid[1] (^): first 5 = {arr[1, :5]}")
        print(f"  sid[10] (n): first 5 = {arr[10, :5]}")
        print(f"  sid[39] (i): first 5 = {arr[39, :5]}")
        print(f"  sid[0] (_): first 5 = {arr[0, :5]}")
        
        # Save embedding for Go comparison
        np.save(os.path.join(os.path.dirname(__file__), "ref_piper_emb.npy"), arr)
        print(f"  Saved embedding to ref_piper_emb.npy")
        break

# Run with noise_scale=0, noise_scale_w=0 for deterministic output
print("\n=== Deterministic inference ===")
sess = ort.InferenceSession(model_path)
phoneme_ids = np.array([[1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2]], dtype=np.int64)
input_lengths = np.array([17], dtype=np.int64)
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)

output = sess.run(None, {
    "input": phoneme_ids,
    "input_lengths": input_lengths,
    "scales": scales,
})
audio = output[0].squeeze()
print(f"Audio: {len(audio)} samples, {len(audio)/22050:.2f}s")
print(f"Mel frames (estimated): {len(audio) // 256} (audio_len / hop_length)")
print(f"Mel frames per phoneme (avg): {len(audio) / 256 / 17:.1f}")

# The expected mel length tells us what the duration predictor should output
expected_mel = len(audio) // 256
print(f"\nExpected total mel frames: ~{expected_mel}")
print(f"Average duration per phoneme: ~{expected_mel / 17:.1f} frames")
