#!/usr/bin/env python3
"""Test maclaw.m4a with official HuggingFace Moonshine pipeline as ground truth."""
import numpy as np, wave, os, sys

os.chdir(os.path.join(os.path.dirname(__file__), ".."))

wav_path = "test/real_speech/maclaw_16k.wav"
if not os.path.exists(wav_path):
    print("Run test_maclaw_real.py first to convert m4a to wav")
    sys.exit(1)

with wave.open(wav_path, "rb") as wf:
    frames = wf.readframes(wf.getnframes())
    pcm = np.frombuffer(frames, dtype=np.int16).astype(np.float32) / 32768.0

print(f"Audio: {len(pcm)/16000:.1f}s")

try:
    from transformers import AutoModelForSpeechSeq2Seq, AutoProcessor
    import torch
except ImportError:
    print("Need: pip install transformers torch")
    sys.exit(1)

for model_name in ["moonshine-tiny", "moonshine-base"]:
    model_dir = f"models/{model_name}"
    print(f"\n{'='*60}")
    print(f"  HF Official: {model_name}")
    print(f"{'='*60}")

    model = AutoModelForSpeechSeq2Seq.from_pretrained(model_dir)
    processor = AutoProcessor.from_pretrained(model_dir)

    inputs = processor(pcm, return_tensors="pt", sampling_rate=16000)

    # Method 1: default generate
    gen1 = model.generate(**inputs, max_length=200)
    text1 = processor.decode(gen1[0], skip_special_tokens=True)
    print(f"  Default:     \"{text1}\"")

    # Method 2: with token limit (from README)
    token_limit_factor = 6.5 / 16000
    seq_len = inputs.attention_mask.sum(dim=-1)
    max_len = int((seq_len * token_limit_factor).max().item())
    gen2 = model.generate(**inputs, max_length=max(max_len, 10))
    text2 = processor.decode(gen2[0], skip_special_tokens=True)
    print(f"  Token-limit: \"{text2}\" (max_len={max_len})")

    # Method 3: greedy no special params
    gen3 = model.generate(**inputs)
    text3 = processor.decode(gen3[0], skip_special_tokens=True)
    print(f"  Greedy:      \"{text3}\"")

print("\nDone.")
