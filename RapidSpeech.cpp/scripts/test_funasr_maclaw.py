#!/usr/bin/env python3
"""Test maclaw.m4a with FunASR (supports Chinese)."""
import os, sys
os.chdir(os.path.join(os.path.dirname(__file__), ".."))

wav_path = "test/real_speech/maclaw_16k.wav"
if not os.path.exists(wav_path):
    print("Need maclaw_16k.wav - run test_maclaw_real.py first")
    sys.exit(1)

from funasr_onnx import Paraformer

model = Paraformer("iic/speech_paraformer-large_asr_nat-zh-cn-16k-common-vocab8404-pytorch", quantize=True)
res = model(wav_path)
print("FunASR paraformer-zh (ONNX) result:")
for r in res:
    print(f"  {r}")
