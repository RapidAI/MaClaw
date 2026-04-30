#!/usr/bin/env python3
"""Quick test of huayan model only."""
import os, tarfile, subprocess
import sherpa_onnx, numpy as np, scipy.io.wavfile as wavfile

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "vits-piper-zh_CN-huayan-medium")

if not os.path.exists(model_dir):
    tar = os.path.join(outdir, "vits-piper-zh_CN-huayan-medium.tar.bz2")
    print("Extracting...")
    import tarfile
    with tarfile.open(tar, "r:bz2") as t:
        t.extractall(outdir)

# espeak-ng-data
espeak = os.path.join(outdir, "espeak-ng-data")
if not os.path.exists(espeak):
    tar = os.path.join(outdir, "espeak-ng-data.tar.bz2")
    if os.path.exists(tar):
        with tarfile.open(tar, "r:bz2") as t:
            t.extractall(outdir)
    else:
        print("Downloading espeak-ng-data...")
        subprocess.run(["curl", "-L", "-o", tar,
            "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/espeak-ng-data.tar.bz2"],
            check=True, timeout=120)
        with tarfile.open(tar, "r:bz2") as t:
            t.extractall(outdir)

print(f"Model dir: {model_dir}")
print(f"Files: {os.listdir(model_dir)}")

model_onnx = os.path.join(model_dir, "model.onnx")
tokens = os.path.join(model_dir, "tokens.txt")

tts = sherpa_onnx.OfflineTts(sherpa_onnx.OfflineTtsConfig(
    model=sherpa_onnx.OfflineTtsModelConfig(
        vits=sherpa_onnx.OfflineTtsVitsModelConfig(
            model=model_onnx, tokens=tokens, data_dir=espeak,
        ), num_threads=4,
    ),
))
print(f"Sample rate: {tts.sample_rate}")

for text in ["你好世界", "今天天气不错", "欢迎使用MacLaw", "我们一起来写代码吧"]:
    audio = tts.generate(text, sid=0, speed=1.0)
    s = np.array(audio.samples)
    print(f"  '{text}': {len(s)/audio.sample_rate:.2f}s, max={abs(s).max():.4f}")
    fname = f"huayan_{text[:4]}.wav"
    wavfile.write(os.path.join(outdir, fname), audio.sample_rate,
                  np.clip(s*32767,-32768,32767).astype(np.int16))
    print(f"  → {fname}")
