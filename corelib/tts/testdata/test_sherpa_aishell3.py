#!/usr/bin/env python3
"""Test aishell3 VITS model with sherpa-onnx, then ASR verify."""
import os, sys, subprocess, tarfile

outdir = os.path.dirname(os.path.abspath(__file__))
model_dir = os.path.join(outdir, "vits-icefall-zh-aishell3")

# Download model if not exists
if not os.path.exists(model_dir):
    tar_path = os.path.join(outdir, "vits-icefall-zh-aishell3.tar.bz2")
    if not os.path.exists(tar_path):
        print("Downloading vits-icefall-zh-aishell3...")
        url = "https://github.com/k2-fsa/sherpa-onnx/releases/download/tts-models/vits-icefall-zh-aishell3.tar.bz2"
        subprocess.run(["curl", "-L", "-o", tar_path, url], check=True)
    print("Extracting...")
    with tarfile.open(tar_path, "r:bz2") as tar:
        tar.extractall(outdir)
    print(f"Extracted to {model_dir}")

# List model files
print(f"\nModel files in {model_dir}:")
for f in os.listdir(model_dir):
    size = os.path.getsize(os.path.join(model_dir, f))
    print(f"  {f}: {size/1024:.0f} KB")

# Test with sherpa-onnx
import sherpa_onnx
import numpy as np

model_path = os.path.join(model_dir, "model.onnx")
lexicon_path = os.path.join(model_dir, "lexicon.txt")
tokens_path = os.path.join(model_dir, "tokens.txt")
rules_path = os.path.join(model_dir, "phone.fst")
rule_fsts = rules_path if os.path.exists(rules_path) else ""

tts_config = sherpa_onnx.OfflineTtsConfig(
    model=sherpa_onnx.OfflineTtsModelConfig(
        vits=sherpa_onnx.OfflineTtsVitsModelConfig(
            model=model_path,
            lexicon=lexicon_path,
            tokens=tokens_path,
        ),
        num_threads=4,
    ),
)

tts = sherpa_onnx.OfflineTts(tts_config)
print(f"\nModel loaded. Sample rate: {tts.sample_rate}")

# Test sentences
tests = [
    "你好世界",
    "今天天气不错",
    "欢迎使用MacLaw",
    "我们一起来写代码吧",
    "Hello world",
]

import scipy.io.wavfile as wavfile

for text in tests:
    print(f"\n--- '{text}' ---")
    audio = tts.generate(text, sid=0, speed=1.0)
    samples = np.array(audio.samples)
    print(f"  Audio: {len(samples)} samples ({len(samples)/audio.sample_rate:.2f}s), "
          f"max={abs(samples).max():.4f}")

    fname = f"aishell3_{text[:6].replace(' ','_')}.wav"
    fpath = os.path.join(outdir, fname)
    samples_int16 = np.clip(samples * 32767, -32768, 32767).astype(np.int16)
    wavfile.write(fpath, audio.sample_rate, samples_int16)
    print(f"  Saved: {fname}")

print("\nDone! Listen to the WAV files to evaluate quality.")
