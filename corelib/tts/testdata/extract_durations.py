#!/usr/bin/env python3
"""Extract actual durations from ONNX model for calibrating Go duration predictor."""
import os, numpy as np
import onnxruntime as ort
import onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Find the Ceil node output (durations before ReduceSum)
# The flow is: SDP → exp → * length_scale → Ceil → ReduceSum (total mel frames)
# We want the Ceil output (integer durations per phoneme)
for node in graph.node:
    if node.op_type == "Ceil":
        ceil_out = node.output[0]
        print(f"Ceil output: {ceil_out}")
        new_out = helper.make_tensor_value_info(ceil_out, TensorProto.FLOAT, None)
        graph.output.append(new_out)
        break

# Also get the exp output (before length_scale multiplication)
for node in graph.node:
    if node.op_type == "Exp" and "dp" not in node.name and "flow" not in node.name:
        # This should be the exp(logw) in the main pipeline
        exp_out = node.output[0]
        # Check if it feeds into Mul (length_scale)
        for n2 in graph.node:
            if n2.op_type == "Mul" and exp_out in n2.input:
                print(f"Exp output (raw durations): {exp_out}")
                new_out2 = helper.make_tensor_value_info(exp_out, TensorProto.FLOAT, None)
                graph.output.append(new_out2)
                break

modified_path = model_path + ".dur_debug.onnx"
onnx.save(model, modified_path)

sess = ort.InferenceSession(modified_path)

test_cases = [
    ("你好世界", [1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2]),
    ("今天天气不错", [1, 15, 45, 64, 0, 9, 44, 64, 0, 9, 44, 64, 0, 16, 39, 67, 0, 4, 49, 67, 0, 23, 51, 67, 2]),
]

for text, pids in test_cases:
    phoneme_ids = np.array([pids], dtype=np.int64)
    input_lengths = np.array([len(pids)], dtype=np.int64)
    scales = np.array([0.667, 1.0, 0.8], dtype=np.float32)
    
    outputs = sess.run(None, {
        "input": phoneme_ids,
        "input_lengths": input_lengths,
        "scales": scales,
    })
    
    audio = outputs[0].squeeze()
    durations = outputs[1].squeeze() if len(outputs) > 1 else None
    raw_durs = outputs[2].squeeze() if len(outputs) > 2 else None
    
    print(f"\n=== {text} ===")
    print(f"Audio: {len(audio)} samples, {len(audio)/22050:.2f}s")
    if durations is not None:
        print(f"Durations (ceil): {durations}")
        print(f"  Total mel frames: {durations.sum():.0f}")
        print(f"  Per phoneme: min={durations.min():.0f}, max={durations.max():.0f}, mean={durations.mean():.1f}")
    if raw_durs is not None:
        print(f"Raw durations (exp): {raw_durs}")
        print(f"  Log durations: {np.log(raw_durs + 1e-8)}")

os.remove(modified_path)
