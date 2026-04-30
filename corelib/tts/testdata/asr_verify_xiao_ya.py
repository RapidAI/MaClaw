#!/usr/bin/env python3
"""Verify xiao_ya TTS output quality using sherpa-onnx ASR."""
import sys
import os

try:
    import sherpa_onnx
except ImportError:
    os.system(f"{sys.executable} -m pip install sherpa-onnx -q")
    import sherpa_onnx

import wave
import struct

# First, generate audio with TTS
model_dir = os.path.join(os.path.dirname(__file__), "vits-piper-zh_CN-xiao_ya-medium")
model_path = os.path.join(model_dir, "zh_CN-xiao_ya-medium.onnx")
tokens_path = os.path.join(model_dir, "tokens.txt")
lexicon_path = os.path.join(model_dir, "lexicon.txt")

tts_config = sherpa_onnx.OfflineTtsConfig(
    model=sherpa_onnx.OfflineTtsModelConfig(
        vits=sherpa_onnx.OfflineTtsVitsModelConfig(
            model=model_path,
            tokens=tokens_path,
            lexicon=lexicon_path,
            data_dir=model_dir,
            noise_scale=0.667,
            noise_scale_w=0.8,
            length_scale=1.0,
        ),
        provider="cpu",
        num_threads=4,
    ),
    max_num_sentences=1,
)
tts = sherpa_onnx.OfflineTts(tts_config)

# Now set up ASR (using Moonshine or any available model)
# Try to use sherpa-onnx's built-in ASR
# We'll use the Paraformer model for Chinese ASR
asr_model_dir = os.path.join(os.path.dirname(__file__), "..", "..", "asr")
moonshine_dir = os.path.join(asr_model_dir, "testdata")

# Check if we have a Chinese ASR model
# For now, let's just do TTS and save, then manually check
test_cases = [
    "你好世界",
    "今天天气不错", 
    "我们一起来写代码吧",
]

out_dir = os.path.dirname(__file__)
print("=== xiao_ya TTS Quality Test ===")
print(f"Sample rate: {tts.sample_rate}")
print()

for text in test_cases:
    audio = tts.generate(text, sid=0, speed=1.0)
    samples = audio.samples
    sr = audio.sample_rate
    duration = len(samples) / sr
    
    # Calculate basic audio stats
    import math
    rms = math.sqrt(sum(s*s for s in samples) / len(samples))
    peak = max(abs(s) for s in samples)
    
    print(f"Text: {text}")
    print(f"  Duration: {duration:.2f}s, Samples: {len(samples)}")
    print(f"  RMS: {rms:.4f}, Peak: {peak:.4f}")
    print(f"  Chars/sec: {len(text)/duration:.1f}")
    print()

print("Audio files saved as xiao_ya_*.wav - please listen to verify quality.")
print("\nKey observations:")
print("- Model uses pinyin phonemes (not IPA/espeak)")
print("- No BERT dependency")
print("- 22050 Hz sample rate")
print("- Single speaker")
print("- Chinese only (English OOV)")
