#!/usr/bin/env python3
"""Test maclaw.m4a with official useful-moonshine package as ground truth."""
import numpy as np, wave, os, sys

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

wav_path = "test/real_speech/maclaw_16k.wav"
if not os.path.exists(wav_path):
    print("Run test_maclaw_real.py first to convert m4a to wav")
    sys.exit(1)

# Load as float32 array
import soundfile as sf
audio, sr = sf.read(wav_path)
if sr != 16000:
    import librosa
    audio = librosa.resample(audio, orig_sr=sr, target_sr=16000)
print(f"Audio: {len(audio)/16000:.1f}s, dtype={audio.dtype}")

from moonshine import transcribe
for model_name in ["moonshine/tiny", "moonshine/base"]:
    print(f"\n{'='*60}")
    print(f"  Official moonshine: {model_name}")
    print(f"{'='*60}")
    result = transcribe(audio, model_name)
    print(f"  Result: {result}")

print("\nDone.")
