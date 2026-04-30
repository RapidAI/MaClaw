#!/usr/bin/env python3
"""Extract flow decoder input (z_p) and output (z) from ONNX for comparison."""
import os, numpy as np
import onnxruntime as ort
import onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Find the flow decoder input and output
# Flow input: z_p (after sampling from prior)
# Flow output: z (before vocoder, = /Mul_7_output_0 which we already know)

# The flow decoder in ONNX is a series of Split → coupling → Concat operations
# Let's find the first Split in the flow (which takes z_p as input)
flow_nodes = []
for node in graph.node:
    if "flow" in node.name.lower():
        flow_nodes.append(node)

# Find the input to the first flow operation
# The flow.flows.6 is processed first (reverse order in ONNX)
for node in graph.node:
    if "/flow/flows.6/pre/Conv" in node.name:
        print(f"Flow.6 pre input: {node.input[0]}")
        # The input to pre is the first half of z_p after Split
        break

# Find the Split that feeds into flow.flows.6
for node in graph.node:
    if node.op_type == "Split" and any("flow" in str(o) for o in node.output):
        print(f"Flow Split: {node.name}, input={node.input[0]}, outputs={list(node.output)}")

# Let's just add the z (vocoder input) and z_p (flow input) as outputs
# z = /Mul_7_output_0 (already known)
# z_p = the tensor that enters the first flow Split

# Find the Randn/sampling operation that produces z_p
# In VITS: z_p = m_p + noise * exp(logs_p)
# Look for the Add that combines m_p_expanded with noise
for node in graph.node:
    if node.op_type == "Flip":
        print(f"Flip: {node.name}, input={node.input[0]}")

# Actually, let's just capture the flow input by finding what feeds into
# the first flow.flows.6 Split
for node in graph.node:
    if node.op_type == "Split":
        for out in node.output:
            # Check if this output feeds into flow.flows.6.pre
            for n2 in graph.node:
                if "/flow/flows.6/pre" in n2.name and out in n2.input:
                    split_input = node.input[0]
                    print(f"\nFound: z_p tensor = {split_input}")
                    # Add as output
                    new_out = helper.make_tensor_value_info(split_input, TensorProto.FLOAT, None)
                    graph.output.append(new_out)
                    
                    # Also add z (vocoder input)
                    z_name = "/Mul_7_output_0"
                    new_out2 = helper.make_tensor_value_info(z_name, TensorProto.FLOAT, None)
                    graph.output.append(new_out2)
                    
                    modified_path = model_path + ".flow_debug.onnx"
                    onnx.save(model, modified_path)
                    
                    # Run with deterministic settings
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
                    z_p = outputs[1].squeeze()
                    z = outputs[2].squeeze()
                    
                    print(f"z_p: shape={z_p.shape}, RMS={np.sqrt(np.mean(z_p**2)):.6f}")
                    print(f"z:   shape={z.shape}, RMS={np.sqrt(np.mean(z**2)):.6f}")
                    print(f"audio: {len(audio)} samples")
                    
                    # Save for Go comparison
                    z_p.astype(np.float32).tofile("corelib/tts/testdata/ref_piper_z_p.bin")
                    z.astype(np.float32).tofile("corelib/tts/testdata/ref_piper_z.bin")
                    print(f"Saved z_p ({z_p.shape}) and z ({z.shape})")
                    
                    # Compare z_p and z distributions
                    print(f"\nz_p stats: mean={z_p.mean():.6f}, std={z_p.std():.6f}")
                    print(f"z   stats: mean={z.mean():.6f}, std={z.std():.6f}")
                    print(f"z_p first 5: {z_p.flatten()[:5]}")
                    print(f"z   first 5: {z.flatten()[:5]}")
                    
                    os.remove(modified_path)
                    exit()
