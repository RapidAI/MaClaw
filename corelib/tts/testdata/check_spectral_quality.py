#!/usr/bin/env python3
"""Check spectral quality of Go vs ONNX output — detect electronic noise."""
import os, wave, struct, numpy as np

def read_wav(path):
    with wave.open(path, 'r') as wf:
        n = wf.getnframes()
        data = wf.readframes(n)
        samples = np.array(struct.unpack(f'<{n}h', data), dtype=np.float32) / 32767.0
        return samples, wf.getframerate()

testdata = os.path.dirname(__file__)

for name in ["你好世界", "今天天气不错"]:
    go_path = os.path.join(testdata, f"go_piper_{name}.wav")
    onnx_path = os.path.join(testdata, f"xiao_ya_{name}.wav")
    
    if not os.path.exists(go_path) or not os.path.exists(onnx_path):
        continue
    
    go_s, sr = read_wav(go_path)
    onnx_s, _ = read_wav(onnx_path)
    
    print(f"\n=== {name} ===")
    
    # Compute spectral energy distribution
    for label, s in [("Go", go_s), ("ONNX", onnx_s)]:
        # FFT
        n = len(s)
        fft = np.fft.rfft(s)
        mag = np.abs(fft)
        freqs = np.fft.rfftfreq(n, 1.0/sr)
        
        # Energy in frequency bands
        total_energy = np.sum(mag**2)
        low = np.sum(mag[freqs < 1000]**2)      # 0-1kHz (fundamental + low harmonics)
        mid = np.sum(mag[(freqs >= 1000) & (freqs < 4000)]**2)  # 1-4kHz (speech formants)
        high = np.sum(mag[(freqs >= 4000) & (freqs < 8000)]**2)  # 4-8kHz (sibilants)
        vhigh = np.sum(mag[freqs >= 8000]**2)    # 8kHz+ (noise/artifacts)
        
        print(f"  {label}: low={100*low/total_energy:.1f}% mid={100*mid/total_energy:.1f}% "
              f"high={100*high/total_energy:.1f}% vhigh={100*vhigh/total_energy:.1f}%")
        
        # High-frequency noise ratio (electronic noise indicator)
        hf_ratio = vhigh / (total_energy + 1e-10)
        if hf_ratio > 0.1:
            print(f"    ⚠️ High HF noise: {100*hf_ratio:.1f}% energy above 8kHz")
        else:
            print(f"    ✓ HF noise OK: {100*hf_ratio:.1f}% energy above 8kHz")
