#!/usr/bin/env python3
"""Compare Go Piper output with ONNX reference output."""
import os
import wave
import struct
import numpy as np

testdata = os.path.dirname(__file__)

texts = ["你好世界", "今天天气不错", "我们一起来写代码吧"]

for text in texts:
    safe = text[:20]
    go_path = os.path.join(testdata, f"go_piper_{safe}.wav")
    onnx_path = os.path.join(testdata, f"xiao_ya_{safe}.wav")
    
    if not os.path.exists(go_path):
        print(f"Go WAV not found: {go_path}")
        continue
    if not os.path.exists(onnx_path):
        print(f"ONNX WAV not found: {onnx_path}")
        continue
    
    # Read WAV files
    def read_wav(path):
        with wave.open(path, 'r') as wf:
            n = wf.getnframes()
            data = wf.readframes(n)
            samples = np.array(struct.unpack(f'<{n}h', data), dtype=np.float32) / 32767.0
            return samples, wf.getframerate()
    
    go_samples, go_sr = read_wav(go_path)
    onnx_samples, onnx_sr = read_wav(onnx_path)
    
    print(f"\n=== {text} ===")
    print(f"  Go:   {len(go_samples)} samples, {go_sr}Hz, {len(go_samples)/go_sr:.2f}s, RMS={np.sqrt(np.mean(go_samples**2)):.4f}, peak={np.max(np.abs(go_samples)):.4f}")
    print(f"  ONNX: {len(onnx_samples)} samples, {onnx_sr}Hz, {len(onnx_samples)/onnx_sr:.2f}s, RMS={np.sqrt(np.mean(onnx_samples**2)):.4f}, peak={np.max(np.abs(onnx_samples)):.4f}")
    print(f"  Duration ratio: {len(go_samples)/len(onnx_samples):.2f}")
    
    # Check if Go output has any signal (not just noise/silence)
    go_rms = np.sqrt(np.mean(go_samples**2))
    if go_rms < 0.001:
        print(f"  ⚠️ Go output appears to be silence (RMS={go_rms:.6f})")
    elif go_rms < 0.01:
        print(f"  ⚠️ Go output is very quiet (RMS={go_rms:.6f})")
    else:
        print(f"  ✓ Go output has signal")
