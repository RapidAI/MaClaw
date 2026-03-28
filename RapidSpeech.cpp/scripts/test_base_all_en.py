#!/usr/bin/env python3
"""Run moonshine-base on all English test WAVs, greedy vs strategy."""
import sys, os
sys.path.insert(0, os.path.dirname(__file__))
os.chdir(os.path.join(os.path.dirname(__file__), ".."))
from test_decode_base import *

files = sorted(Path("test/real_speech").glob("en_*.wav"))
print("Moonshine-BASE: all English test files")
print("=" * 70)
for f in files:
    pcm = load_wav(str(f))
    dur = len(pcm) / 16000
    enc = encode(pcm)
    tg, _ = decode(enc, 200, False)
    ts, _ = decode(enc, 200, True)
    crg = compression_ratio(tg)
    crs = compression_ratio(ts)
    print(f"{f.name} ({dur:.1f}s):")
    print(f'  GREEDY:   {len(tg)-1:3d} tok cr={crg:.1f} "{tokens_to_text(tg)[:100]}"')
    print(f'  STRATEGY: {len(ts)-1:3d} tok cr={crs:.1f} "{tokens_to_text(ts)[:100]}"')
    print()
