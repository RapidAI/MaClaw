#!/usr/bin/env python3
"""Debug: Check if C++ fbank matches PyTorch fbank for the same WAV."""
import os, sys, json, subprocess, numpy as np, torch, soundfile as sf

SERVER = "http://localhost:8090"

def load_wav(path, sr=16000):
    data, orig_sr = sf.read(path, dtype='float32')
    if len(data.shape) > 1: data = data[:, 0]
    if orig_sr != sr:
        import torchaudio
        t = torch.from_numpy(data).unsqueeze(0)
        t = torchaudio.functional.resample(t, orig_sr, sr)
        data = t.squeeze(0).numpy()
    return data

def get_cpp_embedding(wav_path):
    r = subprocess.run(
        ["curl.exe", "-s", "-X", "POST", f"{SERVER}/v1/speaker-embed",
         "-F", f"file=@{wav_path}"],
        capture_output=True, text=True, timeout=60)
    d = json.loads(r.stdout)
    return np.array(d["embedding"])
