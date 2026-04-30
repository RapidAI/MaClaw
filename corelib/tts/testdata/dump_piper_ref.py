#!/usr/bin/env python3
"""Dump intermediate values from Piper ONNX inference for comparison with Go."""
import os
import sys
import numpy as np

try:
    import onnxruntime as ort
except ImportError:
    os.system(f"{sys.executable} -m pip install onnxruntime -q")
    import onnxruntime as ort

model_dir = os.path.join(os.path.dirname(__file__), "vits-piper-zh_CN-xiao_ya-medium")
model_path = os.path.join(model_dir, "zh_CN-xiao_ya-medium.onnx")

# Load model
sess = ort.InferenceSession(model_path)

# Input for "你好世界" — same phoneme IDs as Go output
# Go: [1 10 39 66 0 14 32 66 0 20 39 67 0 15 41 67 2]
phoneme_ids = np.array([[1, 10, 39, 66, 0, 14, 32, 66, 0, 20, 39, 67, 0, 15, 41, 67, 2]], dtype=np.int64)
input_lengths = np.array([17], dtype=np.int64)
scales = np.array([0.667, 1.0, 0.8], dtype=np.float32)  # noise_scale, length_scale, noise_scale_w

print(f"Input phoneme IDs: {phoneme_ids[0].tolist()}")
print(f"Input length: {input_lengths[0]}")
print(f"Scales: noise={scales[0]}, length={scales[1]}, noise_w={scales[2]}")

# Run inference
output = sess.run(None, {
    "input": phoneme_ids,
    "input_lengths": input_lengths,
    "scales": scales,
})

audio = output[0].squeeze()
print(f"\nOutput audio: {audio.shape}, RMS={np.sqrt(np.mean(audio**2)):.4f}, peak={np.max(np.abs(audio)):.4f}")
print(f"Duration: {len(audio)/22050:.2f}s")
print(f"Samples: {len(audio)}")

# Save reference WAV
import wave
import struct
wav_path = os.path.join(os.path.dirname(__file__), "piper_onnx_ref_你好世界.wav")
with wave.open(wav_path, 'w') as wf:
    wf.setnchannels(1)
    wf.setsampwidth(2)
    wf.setframerate(22050)
    pcm = b''
    for s in audio:
        s = max(-1.0, min(1.0, float(s)))
        pcm += struct.pack('<h', int(s * 32767))
    wf.writeframes(pcm)
print(f"Saved: {wav_path}")

# Also try with deterministic settings (noise_scale=0, noise_scale_w=0)
scales_det = np.array([0.0, 1.0, 0.0], dtype=np.float32)
output_det = sess.run(None, {
    "input": phoneme_ids,
    "input_lengths": input_lengths,
    "scales": scales_det,
})
audio_det = output_det[0].squeeze()
print(f"\nDeterministic output: {audio_det.shape}, RMS={np.sqrt(np.mean(audio_det**2)):.4f}, peak={np.max(np.abs(audio_det)):.4f}")
print(f"Duration: {len(audio_det)/22050:.2f}s, Samples: {len(audio_det)}")
