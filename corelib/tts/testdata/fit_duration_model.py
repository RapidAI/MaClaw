#!/usr/bin/env python3
"""Fit a simple duration model from ONNX reference data."""
import numpy as np, onnxruntime as ort, onnx
from onnx import helper, TensorProto

model_path = "corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx"
model = onnx.load(model_path)
graph = model.graph

# Add encoder output and path as outputs
graph.output.append(helper.make_tensor_value_info("/enc_p/Split_output_0", TensorProto.FLOAT, None))
graph.output.append(helper.make_tensor_value_info("/Squeeze_output_0", TensorProto.FLOAT, None))

modified_path = model_path + ".fit.onnx"
onnx.save(model, modified_path)
sess = ort.InferenceSession(modified_path)

# Generate training data from multiple texts
texts_pids = [
    [1,10,39,66,0,14,32,66,0,20,39,67,0,15,41,67,2],  # 你好世界
    [1,15,45,64,0,9,44,64,0,9,44,64,0,16,39,67,0,4,49,67,0,23,51,67,2],  # 今天天气不错
    [1,26,28,66,0,6,35,68,0,25,39,67,0,16,39,66,0,11,30,65,0,17,41,66,0,8,30,67,0,6,27,66,0,4,27,68,2],  # 我们一起来写代码吧
    [1,14,54,64,0,25,47,65,0,20,39,66,0,25,38,67,0,18,39,67,0,10,37,65,0,18,49,67,0,20,33,66,2],  # 欢迎使用智能助手
    [1,21,35,65,0,12,38,64,0,18,39,67,0,10,37,65,0,18,37,67,0,22,30,67,0,12,30,66,0,4,44,67,0,20,39,67,0,15,41,67,2],  # 人工智能正在改变世界
]

all_features = []
all_durations = []
all_pids = []

for pids in texts_pids:
    T = len(pids)
    phoneme_ids = np.array([pids], dtype=np.int64)
    input_lengths = np.array([T], dtype=np.int64)
    scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)
    
    outputs = sess.run(None, {"input": phoneme_ids, "input_lengths": input_lengths, "scales": scales})
    m_p = np.array(outputs[1]).squeeze()  # [192, T]
    path = np.array(outputs[2]).squeeze()  # [tMel, T]
    
    # Extract durations from path
    durations = path.sum(axis=0).astype(int)  # [T]
    
    # Features: per-timestep statistics from m_p
    for t in range(T):
        col = m_p[:, t]
        feat = [
            np.mean(col),
            np.std(col),
            np.max(col),
            np.min(col),
            float(pids[t]),  # phoneme ID
        ]
        all_features.append(feat)
        all_durations.append(durations[t])
        all_pids.append(pids[t])

X = np.array(all_features)
y = np.array(all_durations, dtype=np.float32)
pids_arr = np.array(all_pids)

print(f"Training data: {len(X)} samples")
print(f"Duration range: {y.min():.0f} - {y.max():.0f}, mean={y.mean():.1f}")

# Analyze: what's the average duration per phoneme type?
print("\nPer-phoneme-type average duration:")
for pid in sorted(set(all_pids)):
    mask = pids_arr == pid
    avg = y[mask].mean()
    std = y[mask].std()
    n = mask.sum()
    print(f"  pid={pid:3d}: avg={avg:.1f} std={std:.1f} n={n}")

# Simple linear regression: logw = X @ w + b
from numpy.linalg import lstsq
log_y = np.log(y + 0.1)  # log-duration
X_aug = np.column_stack([X, np.ones(len(X))])
w, residuals, rank, sv = lstsq(X_aug, log_y, rcond=None)
pred = X_aug @ w
mse = np.mean((pred - log_y)**2)
print(f"\nLinear regression MSE: {mse:.4f}")
print(f"Weights: {w}")

# Compare predictions
print("\nPrediction vs actual (first text):")
T0 = len(texts_pids[0])
for t in range(T0):
    actual = all_durations[t]
    predicted = np.exp(pred[t])
    print(f"  pid={all_pids[t]:3d} actual={actual:3d} predicted={predicted:.1f}")

import os; os.remove(modified_path)
