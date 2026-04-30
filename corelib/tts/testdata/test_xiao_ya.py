#!/usr/bin/env python3
"""Test xiao_ya Piper model with sherpa-onnx."""
import sys
import os

try:
    import sherpa_onnx
except ImportError:
    print("Installing sherpa-onnx...")
    os.system(f"{sys.executable} -m pip install sherpa-onnx -q")
    import sherpa_onnx

import numpy as np

model_dir = os.path.join(os.path.dirname(__file__), "vits-piper-zh_CN-xiao_ya-medium")
model_path = os.path.join(model_dir, "zh_CN-xiao_ya-medium.onnx")
tokens_path = os.path.join(model_dir, "tokens.txt")
lexicon_path = os.path.join(model_dir, "lexicon.txt")
data_dir = os.path.join(model_dir)  # for date.fst, number.fst, phone.fst

print(f"Model: {model_path}")
print(f"Tokens: {tokens_path}")
print(f"Lexicon: {lexicon_path}")
print(f"Exists: model={os.path.exists(model_path)}, tokens={os.path.exists(tokens_path)}, lexicon={os.path.exists(lexicon_path)}")

# Check for FST files
for f in ["date.fst", "number.fst", "phone.fst"]:
    p = os.path.join(model_dir, f)
    print(f"  {f}: {os.path.exists(p)}")

tts_config = sherpa_onnx.OfflineTtsConfig(
    model=sherpa_onnx.OfflineTtsModelConfig(
        vits=sherpa_onnx.OfflineTtsVitsModelConfig(
            model=model_path,
            tokens=tokens_path,
            lexicon=lexicon_path,
            data_dir=data_dir,
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
print(f"Sample rate: {tts.sample_rate}")

test_texts = [
    "你好世界",
    "今天天气不错",
    "我们一起来写代码吧",
    "欢迎使用MacLaw智能助手",
    "Hello world, 你好世界",
]

out_dir = os.path.dirname(__file__)
for text in test_texts:
    print(f"\nSynthesizing: {text}")
    audio = tts.generate(text, sid=0, speed=1.0)
    samples = audio.samples
    sr = audio.sample_rate
    print(f"  Samples: {len(samples)}, SR: {sr}, Duration: {len(samples)/sr:.2f}s")
    
    # Save WAV
    safe_name = text[:20].replace(" ", "_").replace("/", "_")
    wav_path = os.path.join(out_dir, f"xiao_ya_{safe_name}.wav")
    
    import wave
    import struct
    with wave.open(wav_path, 'w') as wf:
        wf.setnchannels(1)
        wf.setsampwidth(2)
        wf.setframerate(sr)
        pcm = b''
        for s in samples:
            s = max(-1.0, min(1.0, s))
            pcm += struct.pack('<h', int(s * 32767))
        wf.writeframes(pcm)
    print(f"  Saved: {wav_path}")

print("\nDone! Check the WAV files.")
