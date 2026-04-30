#!/usr/bin/env python3
"""Generate z_p from ONNX m_p/logs_p with ONNX durations, save for Go testing."""
import numpy as np

# Load ONNX m_p and logs_p (shape [192, 17])
m_p = np.fromfile("corelib/tts/testdata/ref_piper_m_p.bin", dtype=np.float32).reshape(192, 17)
logs_p = np.fromfile("corelib/tts/testdata/ref_piper_logs_p.bin", dtype=np.float32).reshape(192, 17)

# ONNX durations for "你好世界" (from extract_durations.py, noise_scale=0.667, noise_w=0.8)
# With noise_scale=0: z_p = m_p_expanded (no noise)
# ONNX deterministic durations: [2, 24, 7, 10, 7, 4, 5, 3, 7, 2, 7, 1, 7, 12, 1, 4, 9]
durations = [2, 24, 7, 10, 7, 4, 5, 3, 7, 2, 7, 1, 7, 12, 1, 4, 9]
tMel = sum(durations)
print(f"tMel = {tMel}")

# Expand m_p by durations
m_p_expanded = np.zeros((192, tMel), dtype=np.float32)
pos = 0
for t, dur in enumerate(durations):
    for d in range(dur):
        m_p_expanded[:, pos] = m_p[:, t]
        pos += 1

# z_p = m_p_expanded (noise_scale=0)
z_p = m_p_expanded
print(f"z_p: {z_p.shape}, RMS={np.sqrt(np.mean(z_p**2)):.6f}")

# Save
z_p.astype(np.float32).tofile("corelib/tts/testdata/ref_piper_z_p_from_onnx_dur.bin")
print(f"Saved z_p with ONNX durations: {z_p.shape}")

# Compare with the z_p we extracted from ONNX runtime
z_p_onnx = np.fromfile("corelib/tts/testdata/ref_piper_z_p.bin", dtype=np.float32).reshape(192, 114)
print(f"\nONNX z_p: {z_p_onnx.shape}, RMS={np.sqrt(np.mean(z_p_onnx**2)):.6f}")
print(f"Our z_p:  {z_p.shape}, RMS={np.sqrt(np.mean(z_p**2)):.6f}")

# Check if they match (they should if ONNX also used noise_scale=0)
if z_p.shape == z_p_onnx.shape:
    diff = np.abs(z_p - z_p_onnx)
    print(f"Max diff: {diff.max():.8f}")
else:
    print(f"Shape mismatch: {z_p.shape} vs {z_p_onnx.shape}")
    # The ONNX z_p has tMel=114, ours has tMel=112. Different durations!
    print(f"ONNX tMel=114, our tMel={tMel}")
