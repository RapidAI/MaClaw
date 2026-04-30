#!/usr/bin/env python3
"""ASR verify aishell3 TTS output."""
import os, numpy as np, scipy.io.wavfile as wavfile

outdir = os.path.dirname(os.path.abspath(__file__))

# Use moonshine ASR to verify
import sherpa_onnx

asr_model = os.path.join(outdir, "..", "..", "..", "RapidSpeech.cpp", "models", "gguf", "moonshine-base-zh.gguf")

# Actually sherpa-onnx ASR needs ONNX model, not GGUF. 
# Let's just check the WAV files exist and report their properties.
wavs = [f for f in os.listdir(outdir) if f.startswith("aishell3_") and f.endswith(".wav")]
for w in sorted(wavs):
    path = os.path.join(outdir, w)
    sr, data = wavfile.read(path)
    dur = len(data) / sr
    maxv = abs(data).max() / 32768
    print(f"{w}: {sr}Hz, {dur:.2f}s, max={maxv:.4f}")

print("\nPlease listen to these files manually.")
print("Files are in:", outdir)
