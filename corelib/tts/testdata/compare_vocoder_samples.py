#!/usr/bin/env python3
"""Compare Go and Python vocoder output sample-by-sample."""
import numpy as np, torch, torch.nn.functional as F, wave, struct
import onnx
from onnx import numpy_helper

# Load weights
m = onnx.load("corelib/tts/testdata/vits-piper-zh_CN-xiao_ya-medium/zh_CN-xiao_ya-medium.onnx")
weights = {}
for init in m.graph.initializer:
    if init.name.startswith("dec."):
        weights[init.name] = numpy_helper.to_array(init)

# Load reference z
z = np.fromfile("corelib/tts/testdata/ref_piper_z.bin", dtype=np.float32).reshape(192, 114)
x = torch.from_numpy(z.copy()).unsqueeze(0)

# Run Python vocoder step by step
w_cp = torch.from_numpy(weights["dec.conv_pre.weight"].copy())
b_cp = torch.from_numpy(weights["dec.conv_pre.bias"].copy())
x = F.conv1d(x, w_cp, b_cp, padding=3)

ups_rates = [8, 8, 4]
ups_ksizes = [16, 16, 8]
ch = 256

for i, (rate, ksize) in enumerate(zip(ups_rates, ups_ksizes)):
    x = F.leaky_relu(x, 0.1)
    newCh = ch // 2
    w_up = torch.from_numpy(weights[f"dec.ups.{i}.weight"].copy())
    b_up = torch.from_numpy(weights[f"dec.ups.{i}.bias"].copy())
    pad = (ksize - rate) // 2
    x = F.conv_transpose1d(x, w_up, b_up, stride=rate, padding=pad)
    ch = newCh
    
    rb_sum = None
    for j in range(3):
        idx = i * 3 + j
        xc = x.clone()
        for k in range(2):
            residual = xc.clone()
            xc = F.leaky_relu(xc, 0.1)
            w_rb = torch.from_numpy(weights[f"dec.resblocks.{idx}.convs.{k}.weight"].copy())
            b_rb = torch.from_numpy(weights[f"dec.resblocks.{idx}.convs.{k}.bias"].copy())
            xc = F.conv1d(xc, w_rb, b_rb, padding=(w_rb.shape[2]-1)//2)
            xc = xc + residual
        if rb_sum is None:
            rb_sum = xc
        else:
            rb_sum = rb_sum + xc
    x = rb_sum / 3.0

x = F.leaky_relu(x, 0.1)
w_post = torch.from_numpy(weights["dec.conv_post.weight"].copy())
py_audio = torch.tanh(F.conv1d(x, w_post, padding=3)).squeeze().detach().numpy()

# Read Go output
def read_wav(p):
    with wave.open(p,'r') as w:
        n=w.getnframes(); d=w.readframes(n)
        return np.array(struct.unpack(f'<{n}h',d),dtype=np.float32)/32767.0

go_audio = read_wav("corelib/tts/testdata/go_piper_vocoder_from_onnx_z.wav")

# Go audio has peak normalization applied, undo it
go_peak = np.max(np.abs(go_audio))
py_peak = np.max(np.abs(py_audio))
print(f"Python audio: {len(py_audio)} samples, RMS={np.sqrt(np.mean(py_audio**2)):.6f}, peak={py_peak:.6f}")
print(f"Go audio:     {len(go_audio)} samples, RMS={np.sqrt(np.mean(go_audio**2)):.6f}, peak={go_peak:.6f}")

# Scale Go audio to match Python peak for fair comparison
if go_peak > 0:
    go_scaled = go_audio * (py_peak / go_peak)
else:
    go_scaled = go_audio

# Compare first 200 samples
print(f"\nFirst 20 samples comparison:")
print(f"{'idx':>5} {'Python':>12} {'Go(scaled)':>12} {'diff':>12}")
for i in range(20):
    if i < len(py_audio) and i < len(go_scaled):
        diff = abs(py_audio[i] - go_scaled[i])
        print(f"{i:5d} {py_audio[i]:12.8f} {go_scaled[i]:12.8f} {diff:12.8f}")

# Max difference
min_len = min(len(py_audio), len(go_scaled))
diffs = np.abs(py_audio[:min_len] - go_scaled[:min_len])
print(f"\nMax diff: {diffs.max():.8f}")
print(f"Mean diff: {diffs.mean():.8f}")
print(f"Correlation: {np.corrcoef(py_audio[:min_len], go_scaled[:min_len])[0,1]:.6f}")

# Spectral comparison
fft_py = np.fft.rfft(py_audio)
fft_go = np.fft.rfft(go_scaled[:len(py_audio)])
mag_py = np.abs(fft_py)
mag_go = np.abs(fft_go)
freqs = np.fft.rfftfreq(len(py_audio), 1.0/22050)
te_py = np.sum(mag_py**2)
te_go = np.sum(mag_go**2)
print(f"\nSpectral energy distribution:")
for label, mag, te in [("Python", mag_py, te_py), ("Go", mag_go, te_go)]:
    lo = np.sum(mag[freqs<1000]**2)
    hi = np.sum(mag[freqs>=8000]**2)
    print(f"  {label}: low={100*lo/te:.1f}% vhigh={100*hi/te:.1f}%")
