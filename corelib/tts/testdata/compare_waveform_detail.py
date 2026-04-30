#!/usr/bin/env python3
"""Detailed waveform comparison: Go vs ONNX runtime for 今天天气不错."""
import numpy as np, onnxruntime as ort, wave, struct, os

def read_wav(p):
    with wave.open(p, 'r') as w:
        n = w.getnframes(); d = w.readframes(n)
        return np.array(struct.unpack(f'<{n}h', d), dtype=np.float32) / 32767.0, w.getframerate()

# Generate ONNX reference with SAME phoneme IDs, deterministic
sess = ort.InferenceSession("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")
pids = [1,15,45,64,0,9,44,64,0,9,44,64,0,16,39,67,0,4,49,67,0,23,51,67,2]
phoneme_ids = np.array([pids], dtype=np.int64)
input_lengths = np.array([len(pids)], dtype=np.int64)

# Deterministic (noise_scale=0) for fair comparison
scales = np.array([0.0, 1.0, 0.0], dtype=np.float32)
outputs = sess.run(None, {"input": phoneme_ids, "input_lengths": input_lengths, "scales": scales})
onnx_audio = outputs[0].squeeze()

path = "corelib/tts/testdata/onnxrt_det_今天天气不错.wav"
with wave.open(path, 'w') as wf:
    wf.setnchannels(1); wf.setsampwidth(2); wf.setframerate(22050)
    pcm = struct.pack(f'<{len(onnx_audio)}h', *[int(max(-1,min(1,float(s)))*32767) for s in onnx_audio])
    wf.writeframes(pcm)

# Also with noise for natural sound
scales_noisy = np.array([0.667, 1.0, 0.8], dtype=np.float32)
outputs2 = sess.run(None, {"input": phoneme_ids, "input_lengths": input_lengths, "scales": scales_noisy})
onnx_noisy = outputs2[0].squeeze()
path2 = "corelib/tts/testdata/onnxrt_noisy_今天天气不错.wav"
with wave.open(path2, 'w') as wf:
    wf.setnchannels(1); wf.setsampwidth(2); wf.setframerate(22050)
    pcm = struct.pack(f'<{len(onnx_noisy)}h', *[int(max(-1,min(1,float(s)))*32767) for s in onnx_noisy])
    wf.writeframes(pcm)

# Read Go output
go_audio, _ = read_wav("corelib/tts/testdata/go_piper_今天天气不错.wav")

print(f"ONNX det:   {len(onnx_audio)} samples, {len(onnx_audio)/22050:.2f}s, RMS={np.sqrt(np.mean(onnx_audio**2)):.4f}")
print(f"ONNX noisy: {len(onnx_noisy)} samples, {len(onnx_noisy)/22050:.2f}s, RMS={np.sqrt(np.mean(onnx_noisy**2)):.4f}")
print(f"Go:         {len(go_audio)} samples, {len(go_audio)/22050:.2f}s, RMS={np.sqrt(np.mean(go_audio**2)):.4f}")

# F0 analysis using autocorrelation
def estimate_f0_segments(audio, sr, hop=512, frame_len=2048):
    """Estimate F0 for each segment."""
    f0s = []
    for start in range(0, len(audio) - frame_len, hop):
        frame = audio[start:start+frame_len]
        # Autocorrelation
        corr = np.correlate(frame, frame, mode='full')
        corr = corr[len(corr)//2:]
        # Find first peak after initial decay
        min_lag = sr // 500  # max F0 = 500 Hz
        max_lag = sr // 50   # min F0 = 50 Hz
        if max_lag >= len(corr):
            max_lag = len(corr) - 1
        segment = corr[min_lag:max_lag]
        if len(segment) == 0:
            f0s.append(0)
            continue
        peak = np.argmax(segment) + min_lag
        if corr[peak] > 0.3 * corr[0]:
            f0s.append(sr / peak)
        else:
            f0s.append(0)
    return np.array(f0s)

print("\nF0 analysis:")
for label, audio in [("ONNX det", onnx_audio), ("ONNX noisy", onnx_noisy), ("Go", go_audio)]:
    f0 = estimate_f0_segments(audio, 22050)
    voiced = f0[f0 > 0]
    if len(voiced) > 0:
        print(f"  {label:12s}: mean F0={voiced.mean():.0f}Hz, range=[{voiced.min():.0f},{voiced.max():.0f}], voiced%={100*len(voiced)/len(f0):.0f}%")
    else:
        print(f"  {label:12s}: no voiced segments detected")

print("\nFiles saved for listening comparison:")
print(f"  {path}")
print(f"  {path2}")
print(f"  corelib/tts/testdata/go_piper_今天天气不错.wav")
